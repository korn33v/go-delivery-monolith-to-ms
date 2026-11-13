package usecase

import (
	"context"
	"errors"
	"log/slog"

	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/order-service/internal/domain"
	"example.com/strangler/order-service/internal/repository"
)

type SagaPublisher interface {
	Send(context.Context, sagacontract.Message) error
}
type Orchestrator struct {
	Store        *repository.SagaStore
	Publisher    SagaPublisher
	DemoFailures bool
}

func NewOrchestrator(store *repository.SagaStore, publisher SagaPublisher, demoFailures bool) *Orchestrator {
	return &Orchestrator{Store: store, Publisher: publisher, DemoFailures: demoFailures}
}

func (s *Orchestrator) Get(ctx context.Context, orderID string) (domain.Saga, error) {
	return s.Store.Get(ctx, orderID)
}

func sagaData(s domain.Saga) sagacontract.Data {
	return sagacontract.Data{ProductID: s.Order.ProductID, Quantity: s.Order.Quantity, AmountCents: s.Order.AmountCents, DemoFailure: s.DemoFailure}
}

func (s *Orchestrator) Create(ctx context.Context, request domain.CreateOrder, failure string) (domain.Saga, error) {
	if err := request.Validate(); err != nil {
		return domain.Saga{}, err
	}
	if request.ProductID > 2 || (failure != "" && (!s.DemoFailures || (failure != "payment" && failure != "refund"))) {
		return domain.Saga{}, domain.ErrInvalid
	}
	// A tiny local price catalogue; no synchronous reservation before payment.
	order := domain.Order{ID: contract.ID("order-"), ProductID: request.ProductID, Quantity: request.Quantity, AmountCents: 9999000 * request.Quantity, Status: "PENDING", Owner: "order-service"}
	saga := domain.Saga{ID: contract.ID("saga-"), Order: order, State: domain.Pending, DemoFailure: failure}
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return saga, err
	}
	defer tx.Rollback()
	if err = s.Store.Insert(ctx, tx, saga); err != nil {
		return saga, err
	}
	if err = s.Store.Save(ctx, tx, saga, "OrderCreated", ""); err != nil {
		return saga, err
	}
	command, err := saga.Start()
	if err != nil {
		return saga, err
	}
	if err = s.Store.Save(ctx, tx, saga, "SagaStarted", command); err != nil {
		return saga, err
	}
	if err = tx.Commit(); err != nil {
		return saga, err
	}
	logSaga(saga, "SagaStarted", command, false)
	// Deliberate teaching boundary: DB commit and Kafka publish are not atomic.
	if err = s.Publisher.Send(ctx, sagacontract.New(saga.ID, order.ID, command, sagaData(saga))); err != nil {
		return saga, domain.ErrPublish
	}
	return saga, nil
}

func (s *Orchestrator) Handle(ctx context.Context, event sagacontract.Message) error {
	if err := event.Validate(); err != nil {
		return err
	}
	switch event.Type {
	case sagacontract.PaymentCharged, sagacontract.PaymentFailed, sagacontract.StockReserved, sagacontract.StockReservationFailed, sagacontract.PaymentRefunded, sagacontract.PaymentRefundFailed:
	default:
		return errors.New("order expects a participant event")
	}
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	saga, err := s.Store.Lock(ctx, tx, event.SagaID)
	if err != nil {
		return err
	}
	want := sagaData(saga)
	got := event.Data
	got.Reason = ""
	if saga.Order.ID != event.OrderID || got != want {
		return errors.New("event does not match saga operation")
	}
	next, duplicate, err := s.Store.Receipt(ctx, tx, event)
	if err != nil {
		return err
	}
	if !duplicate {
		command, err := saga.Apply(event.Type, event.Data.Reason)
		if err != nil {
			return err
		}
		if command != "" {
			m := sagacontract.New(saga.ID, saga.Order.ID, command, sagaData(saga))
			next = &m
		}
		if err = s.Store.Save(ctx, tx, saga, event.Type, command); err != nil {
			return err
		}
		if err = s.Store.Record(ctx, tx, event, next); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	command := ""
	if next != nil {
		command = next.Type
	}
	logSaga(saga, event.Type, command, duplicate)
	// On redelivery, republish the saved decision. Business effects stay idempotent.
	if next != nil {
		return s.Publisher.Send(ctx, *next)
	}
	return nil
}

func logSaga(s domain.Saga, event, next string, duplicate bool) {
	slog.Info("saga transition", "service", "order-service", "order_id", s.Order.ID, "saga_id", s.ID, "message_type", event, "saga_state", s.State, "next_command", next, "duplicate", duplicate)
}
