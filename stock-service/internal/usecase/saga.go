package usecase

import (
	"context"
	"errors"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/stock-service/internal/domain"
	"example.com/strangler/stock-service/internal/repository"
	"log/slog"
)

type SagaPublisher interface {
	Send(context.Context, sagacontract.Message) error
}
type SagaService struct {
	Store     *repository.SagaStore
	Publisher SagaPublisher
}

func NewSaga(store *repository.SagaStore, publisher SagaPublisher) *SagaService {
	return &SagaService{Store: store, Publisher: publisher}
}
func (s *SagaService) Handle(ctx context.Context, m sagacontract.Message) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if m.Type != sagacontract.ReserveStock {
		return errors.New("stock expects ReserveStock")
	}
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := s.Store.Lock(ctx, tx, m.Data.ProductID)
	if err != nil {
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
	if err = tx.Commit(); err != nil {
		return err
	}
	slog.Info("stock command handled", "service", "stock-service", "order_id", m.OrderID, "saga_id", m.SagaID, "message_type", m.Type, "saga_state", "", "result", event.Type, "duplicate", duplicate)
	return s.Publisher.Send(ctx, event)
}
