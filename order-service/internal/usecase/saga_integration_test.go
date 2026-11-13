//go:build saga_integration

package usecase

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"example.com/strangler/internal/deadletter"
	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/outbox"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/internal/testdb"
	"example.com/strangler/order-service/internal/domain"
	"example.com/strangler/order-service/internal/repository"
)

type unavailableOutbox struct{}

func (unavailableOutbox) Enqueue(context.Context, *sql.Tx, sagacontract.Message) error {
	return errors.New("outbox write failed")
}

func TestSagaStateAndOutgoingDecisionCommitAtomically(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("SAGA_ORDER_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://orders:orders_demo@localhost:15432/orders?sslmode=disable"
	}
	db := testdb.Open(t, dsn, repository.SagaSchema, outbox.Schema, deadletter.Schema)
	store := &repository.SagaStore{DB: db}
	service := NewOrchestrator(store, unavailableOutbox{}, true)
	failed, err := service.Create(ctx, domain.CreateOrder{ProductID: 2, Quantity: 1}, "")
	if err == nil {
		t.Fatal("expected failed outbox insert")
	}
	if _, err = store.Get(ctx, failed.Order.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("order survived a rolled back outbox write", err)
	}
	messages := &outbox.Store{DB: db}
	service = NewOrchestrator(store, messages, true)
	saga, err := service.Create(ctx, domain.CreateOrder{ProductID: 2, Quantity: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM outbox_messages WHERE id=$1`, saga.ID+":ChargePayment").Scan(&count); err != nil || count != 1 {
		t.Fatalf("initial intent missing: %d %v", count, err)
	}
	invalid := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.StockReserved, sagaData(saga))
	if err = service.Handle(ctx, invalid); !errors.Is(err, domain.ErrTransition) || !messageerror.IsPermanent(err) {
		t.Fatalf("invalid transition not rejected permanently: %v", err)
	}
	charged := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.PaymentCharged, sagaData(saga))
	service.Outbox = unavailableOutbox{}
	if err = service.Handle(ctx, charged); err == nil {
		t.Fatal("expected enqueue failure")
	}
	saved, _ := store.Get(ctx, saga.Order.ID)
	if saved.State != domain.PaymentPending {
		t.Fatal("transition committed without outgoing command")
	}
	// A new application instance reads only durable data.
	service = NewOrchestrator(&repository.SagaStore{DB: db}, &outbox.Store{DB: db}, true)
	if err = service.Handle(ctx, charged); err != nil {
		t.Fatal(err)
	}
	charged.EventID += ":new-id"
	if err = service.Handle(ctx, charged); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM saga_history WHERE saga_id=$1`, saga.ID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("duplicate transition: %d %v", count, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM outbox_messages WHERE id=$1`, saga.ID+":ReserveStock").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate command intent: %d %v", count, err)
	}
	wrong := charged
	wrong.Data.AmountCents++
	if err = service.Handle(ctx, wrong); !messageerror.IsPermanent(err) {
		t.Fatal("accepted conflicting operation", err)
	}
	failedStock := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.StockReservationFailed, sagaData(saga))
	failedStock.Data.Reason = "insufficient_stock"
	if err = service.Handle(ctx, failedStock); err != nil {
		t.Fatal(err)
	}
	saved, _ = store.Get(ctx, saga.Order.ID)
	if saved.State != domain.Compensating || saved.Order.Status != "PENDING" {
		t.Fatal("cancelled before refund", saved)
	}
	if err = db.QueryRow(`SELECT count(*) FROM outbox_messages WHERE id=$1`, saga.ID+":RefundPayment").Scan(&count); err != nil || count != 1 {
		t.Fatalf("compensation not queued: %d %v", count, err)
	}
	refunded := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.PaymentRefunded, sagaData(saga))
	for i := 0; i < 2; i++ {
		if err = service.Handle(ctx, refunded); err != nil {
			t.Fatal(err)
		}
	}
	saved, _ = store.Get(ctx, saga.Order.ID)
	if saved.State != domain.Cancelled || saved.Order.Status != "CANCELLED" {
		t.Fatal(saved)
	}
}
