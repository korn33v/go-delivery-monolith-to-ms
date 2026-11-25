//go:build saga_assignment && saga_integration

package usecase

import (
	"context"
	"testing"

	"example.com/strangler/internal/deadletter"
	"example.com/strangler/internal/outbox"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/internal/testdb"
	"example.com/strangler/stock-service/internal/repository"
)

func TestAssignmentReleaseRestoresInventory(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, "postgres://stock:stock_demo@localhost:15432/stock?sslmode=disable",
		repository.SagaSchema, outbox.Schema, deadletter.Schema, repository.ReleaseSchema)
	store := &repository.SagaStore{DB: db}
	service := NewSaga(store, &outbox.Store{DB: db})
	initial, err := store.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	reserve := sagacontract.New("saga-release", "order-release", sagacontract.ReserveStock,
		sagacontract.Data{ProductID: 1, Quantity: 2, AmountCents: 19998000})
	if err := service.Handle(ctx, reserve); err != nil {
		t.Fatalf("setup: reserve stock: %v", err)
	}
	reserved, err := store.Get(1)
	if err != nil || reserved.Available != initial.Available-2 {
		t.Fatalf("setup: want stock %d after reservation, got %d, err=%v", initial.Available-2, reserved.Available, err)
	}

	release := sagacontract.New(reserve.SagaID, reserve.OrderID, sagacontract.ReleaseStock, reserve.Data)
	if err := service.Handle(ctx, release); err != nil {
		t.Fatalf("AC-03: expected ReleaseStock to succeed for a committed reservation: %v", err)
	}
	restored, err := store.Get(1)
	if err != nil || restored.Available != initial.Available {
		t.Fatalf("AC-03: want stock restored to %d, got %d, err=%v", initial.Available, restored.Available, err)
	}
	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM stock_receipts WHERE order_id=$1", reserve.OrderID).Scan(&status); err != nil || status != "RELEASED" {
		t.Fatalf("AC-03: want stored RELEASED, got %q, err=%v", status, err)
	}
	var confirmation bool
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM outbox_messages WHERE id=$1 AND topic=$2 AND message_key=$3 AND payload->>'message_type'=$4)`,
		release.SagaID+":StockReleased", sagacontract.Events, release.SagaID, sagacontract.StockReleased).Scan(&confirmation)
	if err != nil || !confirmation {
		t.Fatalf("AC-03: want a StockReleased event in Outbox, got %v, err=%v", confirmation, err)
	}
}
