package usecase

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/stock-service/internal/domain"
	"example.com/strangler/stock-service/internal/repository"
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
	if m.Type != sagacontract.ReserveStock && m.Type != sagacontract.ReleaseStock {
		return messageerror.Permanent(errors.New("stock expects ReserveStock or ReleaseStock"))
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
	var event sagacontract.Message
	var duplicate bool
	if m.Type == sagacontract.ReleaseStock {
		event, duplicate, err = s.Store.Release(ctx, tx, p, m)
	} else {
		event, duplicate, err = s.reserve(ctx, tx, p, m)
	}
	if err != nil {
		return err
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

func (s *SagaService) reserve(ctx context.Context, tx *sql.Tx, p domain.Product, m sagacontract.Message) (sagacontract.Message, bool, error) {
	event, duplicate, err := s.Store.Result(ctx, tx, m)
	if err != nil || duplicate {
		return event, duplicate, err
	}
	kind, reason := domain.ReserveAvailable(&p, m.Data.Quantity)
	event = sagacontract.Reply(m, kind, reason)
	return event, false, s.Store.Save(ctx, tx, p, m, event)
}
