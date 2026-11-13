//go:build saga_integration

package usecase

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"

	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/deadletter"
	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/outbox"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/internal/testdb"
	"example.com/strangler/payment-service/internal/repository"
)

type unavailableOutbox struct{}

func (unavailableOutbox) Enqueue(context.Context, *sql.Tx, sagacontract.Message) error {
	return errors.New("outbox unavailable")
}

func TestSagaPaymentAtomicOutboxAndConcurrentRedelivery(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("SAGA_PAYMENT_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://payments:payments_demo@localhost:15432/payments?sslmode=disable"
	}
	db := testdb.Open(t, dsn, repository.SagaSchema, outbox.Schema, deadletter.Schema)
	m := sagacontract.New(contract.ID("saga-test-"), contract.ID("order-test-"), sagacontract.ChargePayment, sagacontract.Data{ProductID: 1, Quantity: 1, AmountCents: 100})
	service := NewSaga(&repository.SagaStore{DB: db}, unavailableOutbox{}, true)
	if err := service.Handle(ctx, m); err == nil {
		t.Fatal("expected enqueue failure")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM payments`).Scan(&count); err != nil || count != 0 {
		t.Fatal("charge committed without outgoing event", err)
	}
	service = NewSaga(&repository.SagaStore{DB: db}, &outbox.Store{DB: db}, true)
	if err := service.Handle(ctx, m); err != nil {
		t.Fatal(err)
	}
	service = NewSaga(&repository.SagaStore{DB: db}, &outbox.Store{DB: db}, true)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := service.Handle(ctx, m); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	wrong := m
	wrong.Data.AmountCents++
	if err := service.Handle(ctx, wrong); !messageerror.IsPermanent(err) {
		t.Fatal("accepted payment key conflict", err)
	}
	m.Type = sagacontract.RefundPayment
	m.EventID = m.SagaID + ":RefundPayment"
	service.Outbox = unavailableOutbox{}
	if err := service.Handle(ctx, m); err == nil {
		t.Fatal("expected refund enqueue failure")
	}
	var refunds int
	if err := db.QueryRow(`SELECT refunds FROM payments WHERE order_id=$1`, m.OrderID).Scan(&refunds); err != nil || refunds != 0 {
		t.Fatal("refund survived rollback", err)
	}
	service.Outbox = &outbox.Store{DB: db}
	if err := service.Handle(ctx, m); err != nil {
		t.Fatal(err)
	}
	m.EventID += ":new-id"
	if err := service.Handle(ctx, m); err != nil {
		t.Fatal(err)
	}
	var state string
	var charges int
	if err := db.QueryRow(`SELECT state,charges,refunds FROM payments WHERE order_id=$1`, m.OrderID).Scan(&state, &charges, &refunds); err != nil {
		t.Fatal(err)
	}
	if state != "REFUNDED" || charges != 1 || refunds != 1 {
		t.Fatalf("double effect: %s %d %d", state, charges, refunds)
	}
	if err := db.QueryRow(`SELECT count(*) FROM outbox_messages`).Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate event intents", count, err)
	}
}
