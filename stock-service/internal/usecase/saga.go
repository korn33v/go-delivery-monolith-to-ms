package usecase

import (
	"context"
	"database/sql"
	"errors"
	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/stock-service/internal/domain"
	"example.com/strangler/stock-service/internal/repository"
	"log/slog"
)

type SagaOutbox interface {
	Enqueue(context.Context, *sql.Tx, sagacontract.Message) error
}
type SagaService struct {
	Store  *repository.SagaStore
	Outbox SagaOutbox
}

func NewSaga(store *repository.SagaStore, messages SagaOutbox) *SagaService {
	return &SagaService{Store: store, Outbox: messages}
}
func (s *SagaService) Handle(ctx context.Context, m sagacontract.Message) error {
	if err := m.Validate(); err != nil {
		return messageerror.Permanent(err)
	}
	if m.Type != sagacontract.ReserveStock {
		// TODO для студентов: обработать ReleaseStock отдельной операцией.
		return messageerror.Permanent(errors.New("stock expects ReserveStock"))
	}
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := s.Store.Lock(ctx, tx, m.Data.ProductID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return messageerror.Permanent(err)
		}
		return err
	}
	event, duplicate, err := s.Store.Result(ctx, tx, m)
	if err != nil {
		return err
	}
	if !duplicate {
		kind, reason := domain.ReserveAvailable(&p, m.Data.Quantity)
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
	slog.Info("stock command handled", "service", "stock-service", "order_id", m.OrderID, "saga_id", m.SagaID, "message_type", m.Type, "saga_state", "", "result", event.Type, "duplicate", duplicate)
	return nil
}
