//go:build saga_integration

package usecase

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"example.com/strangler/internal/deadletter"
	"example.com/strangler/internal/outbox"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/internal/testdb"
	"example.com/strangler/stock-service/internal/repository"
)

type unavailableOutbox struct{}

func (unavailableOutbox) Enqueue(context.Context, *sql.Tx, sagacontract.Message) error {
	return errors.New("outbox unavailable")
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
