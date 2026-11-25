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

var errOutboxWrite = errors.New("outbox write failed")

type unavailableOutbox struct{ calls int }

func (o *unavailableOutbox) Enqueue(context.Context, *sql.Tx, sagacontract.Message) error {
	o.calls++
	return errOutboxWrite
}

func orderTestDSN() string {
	if dsn := os.Getenv("SAGA_ORDER_TEST_DSN"); dsn != "" {
		return dsn
	}
	return "postgres://orders:orders_demo@localhost:15432/orders?sslmode=disable"
}

func TestSagaStateAndOutgoingDecisionCommitAtomically(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, orderTestDSN(), repository.SagaSchema, outbox.Schema, deadletter.Schema, repository.PaymentOrderSchema)
	store := &repository.SagaStore{DB: db}
	initialOutbox := &unavailableOutbox{}
	service := NewOrchestrator(store, initialOutbox, true)
	failed, err := service.Create(ctx, domain.CreateOrder{ProductID: 1, Quantity: 1}, "payment")
	if !errors.Is(err, errOutboxWrite) || initialOutbox.calls != 1 {
		t.Fatalf("create: want one Enqueue and its write error; calls=%d err=%v", initialOutbox.calls, err)
	}
	if _, err = store.Get(ctx, failed.Order.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("order survived a rolled back outbox write", err)
	}
	messages := &outbox.Store{DB: db}
	service = NewOrchestrator(store, messages, true)
	saga, err := service.Create(ctx, domain.CreateOrder{ProductID: 1, Quantity: 1}, "payment")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM outbox_messages WHERE id=$1`, saga.ID+":ReserveStock").Scan(&count); err != nil || count != 1 {
		t.Fatalf("initial intent missing: %d %v", count, err)
	}
	invalid := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.PaymentCharged, sagaData(saga))
	if err = service.Handle(ctx, invalid); !errors.Is(err, domain.ErrTransition) || !messageerror.IsPermanent(err) {
		t.Fatalf("invalid transition not rejected permanently: %v", err)
	}
	reserved := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.StockReserved, sagaData(saga))
	chargeOutbox := &unavailableOutbox{}
	service.Outbox = chargeOutbox
	if err = service.Handle(ctx, reserved); !errors.Is(err, errOutboxWrite) || chargeOutbox.calls != 1 {
		t.Fatalf("StockReserved: want one Enqueue and its write error; calls=%d err=%v", chargeOutbox.calls, err)
	}
	saved, err := store.Get(ctx, saga.Order.ID)
	if err != nil || saved.State != domain.StockPending {
		t.Fatalf("after enqueue failure: state=%s err=%v", saved.State, err)
	}
	// A new application instance reads only durable data.
	service = NewOrchestrator(&repository.SagaStore{DB: db}, &outbox.Store{DB: db}, true)
	if err = service.Handle(ctx, reserved); err != nil {
		t.Fatal(err)
	}
	reserved.EventID += ":new-id"
	if err = service.Handle(ctx, reserved); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM saga_history WHERE saga_id=$1`, saga.ID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("duplicate transition: %d %v", count, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM outbox_messages WHERE id=$1`, saga.ID+":ChargePayment").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate command intent: %d %v", count, err)
	}
	wrong := reserved
	wrong.Data.AmountCents++
	if err = service.Handle(ctx, wrong); !messageerror.IsPermanent(err) {
		t.Fatal("accepted conflicting operation", err)
	}
	declined := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.PaymentFailed, sagaData(saga))
	declined.Data.Reason = "demo_payment_declined"
	if err = service.Handle(ctx, declined); err != nil {
		t.Fatal(err)
	}
	saved, err = store.Get(ctx, saga.Order.ID)
	if err != nil || saved.State != domain.Compensating || saved.Order.Status != "PENDING" {
		t.Fatalf("expected pending compensation: %+v err=%v", saved, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM outbox_messages WHERE id=$1`, saga.ID+":ReleaseStock").Scan(&count); err != nil || count != 1 {
		t.Fatalf("compensation not queued: %d %v", count, err)
	}
	released := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.StockReleased, sagaData(saga))
	for i := 0; i < 2; i++ {
		if err = service.Handle(ctx, released); err != nil {
			t.Fatal(err)
		}
	}
	saved, err = store.Get(ctx, saga.Order.ID)
	if err != nil || saved.State != domain.Cancelled || saved.Order.Status != "CANCELLED" || saved.Reason != "demo_payment_declined" {
		t.Fatalf("expected cancellation after stock release: %+v err=%v", saved, err)
	}
}

func TestExistingSagaKeepsPaymentFirstAfterMigration(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, orderTestDSN(), repository.SagaSchema, outbox.Schema, deadletter.Schema)
	if _, err := db.Exec(`
		INSERT INTO orders VALUES ('old-order',2,1,9999000,'PENDING');
		INSERT INTO sagas VALUES ('old-saga','old-order','PAYMENT_PENDING','','');
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(repository.PaymentOrderSchema); err != nil {
		t.Fatal(err)
	}
	service := NewOrchestrator(&repository.SagaStore{DB: db}, &outbox.Store{DB: db}, true)
	saga, err := service.Get(ctx, "old-order")
	if err != nil || !saga.PaymentFirst {
		t.Fatalf("existing Saga must keep its original flow: %+v, err=%v", saga, err)
	}
	for _, event := range []struct{ kind, reason string }{
		{sagacontract.PaymentCharged, ""},
		{sagacontract.StockReservationFailed, "insufficient_stock"},
		{sagacontract.PaymentRefunded, ""},
	} {
		m := sagacontract.New(saga.ID, saga.Order.ID, event.kind, sagaData(saga))
		m.Data.Reason = event.reason
		if err = service.Handle(ctx, m); err != nil {
			t.Fatalf("old Saga rejected %s: %v", event.kind, err)
		}
	}
	saga, err = service.Get(ctx, saga.Order.ID)
	if err != nil || saga.State != domain.Cancelled || saga.Order.Status != "CANCELLED" {
		t.Fatalf("old Saga did not complete its refund: %+v, err=%v", saga, err)
	}
}
