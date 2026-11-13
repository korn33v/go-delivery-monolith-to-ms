//go:build saga_integration

package usecase

import (
	"context"
	"errors"
	"os"
	"testing"

	"example.com/strangler/internal/postgres"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/order-service/internal/domain"
	"example.com/strangler/order-service/internal/repository"
)

type sagaCapture struct {
	messages []sagacontract.Message
	fail     bool
}

func (p *sagaCapture) Send(_ context.Context, m sagacontract.Message) error {
	p.messages = append(p.messages, m)
	if p.fail {
		return errors.New("broker unavailable")
	}
	return nil
}

func TestSagaPersistsAndRetriesDecisionAfterPublishFailure(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("SAGA_ORDER_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://orders:orders_demo@localhost:15432/orders?sslmode=disable"
	}
	db, err := postgres.Open(ctx, dsn, repository.SagaSchema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := &repository.SagaStore{DB: db}
	publisher := &sagaCapture{fail: true}
	service := NewOrchestrator(store, publisher, true)
	saga, err := service.Create(ctx, domain.CreateOrder{ProductID: 2, Quantity: 1}, "")
	if !errors.Is(err, domain.ErrPublish) {
		t.Fatalf("publish failure must not mean business rejection: %v", err)
	}
	t.Cleanup(func() {
		tx, err := db.Begin()
		if err != nil {
			t.Error(err)
			return
		}
		defer tx.Rollback()
		for _, q := range []string{`DELETE FROM saga_receipts WHERE saga_id=$1`, `DELETE FROM saga_history WHERE saga_id=$1`, `DELETE FROM sagas WHERE id=$1`} {
			if _, err = tx.Exec(q, saga.ID); err != nil {
				t.Error(err)
				return
			}
		}
		if _, err = tx.Exec(`DELETE FROM orders WHERE id=$1`, saga.Order.ID); err != nil {
			t.Error(err)
			return
		}
		if err = tx.Commit(); err != nil {
			t.Error(err)
		}
	})
	saved, err := store.Get(ctx, saga.Order.ID)
	if err != nil || saved.State != domain.PaymentPending || saved.Order.Status != "PENDING" {
		t.Fatalf("lost persisted order: %+v %v", saved, err)
	}
	// A forged result for a future step must roll back without recording a receipt.
	invalid := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.StockReserved, sagaData(saga))
	if err = service.Handle(ctx, invalid); !errors.Is(err, domain.ErrTransition) {
		t.Fatalf("accepted invalid transition: %v", err)
	}
	charged := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.PaymentCharged, sagaData(saga))
	if err = service.Handle(ctx, charged); err == nil {
		t.Fatal("expected technical publish error")
	}
	saved, _ = store.Get(ctx, saga.Order.ID)
	if saved.State != domain.StockPending {
		t.Fatal("local transition was not committed")
	}
	firstDecision := publisher.messages[len(publisher.messages)-1]
	// New application instance: only PostgreSQL survives; no memory dedup cache.
	publisher = &sagaCapture{}
	service = NewOrchestrator(&repository.SagaStore{DB: db}, publisher, true)
	if err = service.Handle(ctx, charged); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 1 || publisher.messages[0] != firstDecision {
		t.Fatal("saved decision was not republished intact")
	}
	charged.EventID += ":new-id"
	if err = service.Handle(ctx, charged); err != nil {
		t.Fatal(err)
	}
	var history int
	if err = db.QueryRow(`SELECT count(*) FROM saga_history WHERE saga_id=$1`, saga.ID).Scan(&history); err != nil || history != 3 {
		t.Fatalf("duplicate transition: count=%d %v", history, err)
	}
	wrong := charged
	wrong.Data.AmountCents++
	if err = service.Handle(ctx, wrong); err == nil {
		t.Fatal("accepted conflicting operation")
	}
	failed := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.StockReservationFailed, sagaData(saga))
	failed.Data.Reason = "insufficient_stock"
	if err = service.Handle(ctx, failed); err != nil {
		t.Fatal(err)
	}
	saved, _ = store.Get(ctx, saga.Order.ID)
	if saved.State != domain.Compensating || saved.Order.Status != "PENDING" {
		t.Fatal("cancelled before refund", saved)
	}
	if publisher.messages[len(publisher.messages)-1].Type != sagacontract.RefundPayment {
		t.Fatal("missing compensation command")
	}
	refunded := sagacontract.New(saga.ID, saga.Order.ID, sagacontract.PaymentRefunded, sagaData(saga))
	if err = service.Handle(ctx, refunded); err != nil {
		t.Fatal(err)
	}
	if err = service.Handle(ctx, refunded); err != nil {
		t.Fatal(err)
	}
	saved, _ = store.Get(ctx, saga.Order.ID)
	if saved.State != domain.Cancelled || saved.Order.Status != "CANCELLED" {
		t.Fatal(saved)
	}
}
