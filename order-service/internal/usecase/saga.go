package usecase

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/order-service/internal/domain"
	"example.com/strangler/order-service/internal/repository"
)

type SagaOutbox interface {
	Enqueue(context.Context, *sql.Tx, sagacontract.Message) error
}
type Orchestrator struct {
	Store        *repository.SagaStore
	Outbox       SagaOutbox
	DemoFailures bool
}

func NewOrchestrator(store *repository.SagaStore, messages SagaOutbox, demoFailures bool) *Orchestrator {
	return &Orchestrator{Store: store, Outbox: messages, DemoFailures: demoFailures}
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
	// Local pricing keeps order creation independent of the Stock service.
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
	if err = s.Outbox.Enqueue(ctx, tx, sagacontract.New(saga.ID, order.ID, command, sagaData(saga))); err != nil {
		return saga, err
	}
	if err = tx.Commit(); err != nil {
		return saga, err
	}
	logSaga(saga, "SagaStarted", command, false)
	return saga, nil
}

func (s *Orchestrator) Handle(ctx context.Context, event sagacontract.Message) error {
	if err := event.Validate(); err != nil {
		return messageerror.Permanent(err)
	}
	switch event.Type {
	case sagacontract.PaymentCharged, sagacontract.PaymentFailed, sagacontract.StockReserved, sagacontract.StockReservationFailed, sagacontract.StockReleased, sagacontract.PaymentRefunded, sagacontract.PaymentRefundFailed:
	default:
		return messageerror.Permanent(errors.New("order expects a participant event"))
	}
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	saga, err := s.Store.Lock(ctx, tx, event.SagaID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return messageerror.Permanent(err)
		}
		return err
	}
	want := sagaData(saga)
	got := event.Data
	got.Reason = ""
	if saga.Order.ID != event.OrderID || got != want {
		return messageerror.Permanent(errors.New("event does not match saga operation"))
	}
	next, duplicate, err := s.Store.Receipt(ctx, tx, event)
	if err != nil {
		return err
	}
	if !duplicate {
		command, err := saga.Apply(event.Type, event.Data.Reason)
		if err != nil {
			return messageerror.Permanent(err)
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
	if next != nil {
		if err = s.Outbox.Enqueue(ctx, tx, *next); err != nil {
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
	return nil
}

func logSaga(s domain.Saga, event, next string, duplicate bool) {
	slog.Info("saga transition", "service", "order-service", "order_id", s.Order.ID, "saga_id", s.ID, "message_type", event, "saga_state", s.State, "next_command", next, "duplicate", duplicate)
}
