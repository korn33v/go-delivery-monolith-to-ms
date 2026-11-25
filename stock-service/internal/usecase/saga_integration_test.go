//go:build saga_integration

package usecase

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"example.com/strangler/internal/deadletter"
	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/outbox"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/internal/testdb"
	"example.com/strangler/stock-service/internal/repository"
)

type unavailableOutbox struct{}

var errOutboxUnavailable = errors.New("outbox unavailable")

func (unavailableOutbox) Enqueue(context.Context, *sql.Tx, sagacontract.Message) error {
	return errOutboxUnavailable
}

func TestStockReleaseRollsBackAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, "postgres://stock:stock_demo@localhost:15432/stock?sslmode=disable",
		repository.SagaSchema, outbox.Schema, deadletter.Schema, repository.ReleaseSchema)
	store := &repository.SagaStore{DB: db}
	service := NewSaga(store, &outbox.Store{DB: db})
	data := sagacontract.Data{ProductID: 1, Quantity: 2, AmountCents: 19998000}
	reserve := sagacontract.New("release-saga", "release-order", sagacontract.ReserveStock, data)
	release := sagacontract.New(reserve.SagaID, reserve.OrderID, sagacontract.ReleaseStock, data)

	if err := service.Handle(ctx, release); !messageerror.IsPermanent(err) {
		t.Fatalf("release without reservation must be rejected, got %v", err)
	}
	if err := service.Handle(ctx, reserve); err != nil {
		t.Fatal(err)
	}
	wrong := release
	wrong.Data.Quantity++
	if err := service.Handle(ctx, wrong); !messageerror.IsPermanent(err) {
		t.Fatalf("release with different reservation data must be rejected, got %v", err)
	}
	failing := NewSaga(store, unavailableOutbox{})
	if err := failing.Handle(ctx, release); !errors.Is(err, errOutboxUnavailable) {
		t.Fatalf("expected release to fail at the Outbox, got %v", err)
	}
	reserved, err := store.Get(data.ProductID)
	if err != nil || reserved.Available != 999998 {
		t.Fatalf("release changed inventory before commit: %+v err=%v", reserved, err)
	}
	var status string
	var hasResponse bool
	if err := db.QueryRow(`SELECT status,release_response IS NOT NULL FROM stock_receipts WHERE order_id=$1`, reserve.OrderID).
		Scan(&status, &hasResponse); err != nil || status != "RESERVED" || hasResponse {
		t.Fatalf("release changed reservation before commit: status=%q response=%v err=%v", status, hasResponse, err)
	}

	duplicate := release
	duplicate.EventID += ":retry"
	results := make(chan error, 2)
	go func() { results <- service.Handle(ctx, release) }()
	go func() { results <- service.Handle(ctx, duplicate) }()
	for i := 0; i < cap(results); i++ {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if err := service.Handle(ctx, reserve); err != nil {
		t.Fatalf("late reserve redelivery failed: %v", err)
	}
	restored, err := store.Get(data.ProductID)
	if err != nil || restored.Available != 1000000 {
		t.Fatalf("retries changed inventory: %+v err=%v", restored, err)
	}
	var intents int
	if err := db.QueryRow(`SELECT count(*) FROM outbox_messages WHERE payload->>'message_type'='StockReleased'`).
		Scan(&intents); err != nil || intents != 1 {
		t.Fatalf("expected one release event, got %d err=%v", intents, err)
	}
}

func TestStockReservationAndOutboxAreAtomic(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, "postgres://stock:stock_demo@localhost:15432/stock?sslmode=disable", repository.SagaSchema, outbox.Schema, deadletter.Schema, repository.ReleaseSchema)
	store := &repository.SagaStore{DB: db}
	m := sagacontract.New("saga-test", "order-test", sagacontract.ReserveStock, sagacontract.Data{ProductID: 1, Quantity: 2, AmountCents: 19998000})
	service := NewSaga(store, unavailableOutbox{})
	if err := service.Handle(ctx, m); err == nil {
		t.Fatal("expected enqueue failure")
	}
	p, err := store.Get(1)
	if err != nil || p.Available != 1000000 {
		t.Fatal("inventory changed after rollback", p, err)
	}
	service.Outbox = &outbox.Store{DB: db}
	for i := 0; i < 2; i++ {
		if err = service.Handle(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	p, err = store.Get(1)
	if err != nil || p.Available != 999998 {
		t.Fatal("duplicate reservation", p, err)
	}
	var intents int
	if err = db.QueryRow(`SELECT count(*) FROM outbox_messages`).Scan(&intents); err != nil || intents != 1 {
		t.Fatal("duplicate stock event", intents, err)
	}
}
