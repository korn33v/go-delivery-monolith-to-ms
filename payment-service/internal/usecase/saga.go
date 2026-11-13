package usecase

import (
	"context"
	"database/sql"
	"errors"
	"example.com/strangler/internal/messageerror"
	"log/slog"

	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/payment-service/internal/repository"
)

type SagaOutbox interface {
	Enqueue(context.Context, *sql.Tx, sagacontract.Message) error
}
type SagaService struct {
	Store        *repository.SagaStore
	Outbox       SagaOutbox
	DemoFailures bool
}

func NewSaga(store *repository.SagaStore, messages SagaOutbox, demoFailures bool) *SagaService {
	return &SagaService{Store: store, Outbox: messages, DemoFailures: demoFailures}
}
func (s *SagaService) Handle(ctx context.Context, m sagacontract.Message) error {
	if err := m.Validate(); err != nil {
		return messageerror.Permanent(err)
	}
	if m.Type != sagacontract.ChargePayment && m.Type != sagacontract.RefundPayment {
		return messageerror.Permanent(errors.New("payment expects ChargePayment or RefundPayment"))
	}
	if m.Data.DemoFailure != "" && !s.DemoFailures {
		return messageerror.Permanent(errors.New("demo failures disabled"))
	}
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := s.Store.Lock(ctx, tx, m)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return messageerror.Permanent(err)
		}
		return err
	}
	event, duplicate, err := s.Store.Result(ctx, tx, m)
	if err != nil {
		return err
	}
	if !duplicate {
		var kind, reason string
		if m.Type == sagacontract.ChargePayment {
			kind, reason, err = p.Charge()
		} else {
			kind, reason, err = p.Refund()
		}
		if err != nil {
			return messageerror.Permanent(err)
		}
		event = sagacontract.Reply(m, kind, reason)
		if err = s.Store.Save(ctx, tx, p, m, event); err != nil {
			return err
		}
	}
	if err = s.Outbox.Enqueue(ctx, tx, event); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	slog.Info("payment command handled", "service", "payment-service", "order_id", m.OrderID, "saga_id", m.SagaID, "message_type", m.Type, "saga_state", "", "payment_state", p.State, "result", event.Type, "duplicate", duplicate)
	return nil
}
