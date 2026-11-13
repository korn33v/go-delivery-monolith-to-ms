//go:build saga_integration

package usecase

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/postgres"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/payment-service/internal/repository"
)

type sagaCapture struct {
	mu       sync.Mutex
	messages []sagacontract.Message
	fail     bool
}

func (p *sagaCapture) Send(_ context.Context, m sagacontract.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, m)
	if p.fail {
		return errors.New("broker unavailable")
	}
	return nil
}

func TestSagaPaymentSurvivesPublishFailureAndConcurrentRedelivery(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("SAGA_PAYMENT_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://payments:payments_demo@localhost:15432/payments?sslmode=disable"
	}
	db, err := postgres.Open(ctx, dsn, repository.SagaSchema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m := sagacontract.New(contract.ID("saga-test-"), contract.ID("order-test-"), sagacontract.ChargePayment, sagacontract.Data{ProductID: 1, Quantity: 1, AmountCents: 100})
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM payment_receipts WHERE order_id=$1`, m.OrderID); err != nil {
			t.Error(err)
		}
		if _, err := db.Exec(`DELETE FROM payments WHERE order_id=$1`, m.OrderID); err != nil {
			t.Error(err)
		}
	})
	publisher := &sagaCapture{fail: true}
	service := NewSaga(&repository.SagaStore{DB: db}, publisher, true)
	if err = service.Handle(ctx, m); err == nil {
		t.Fatal("expected publication failure")
	}
	firstResult := publisher.messages[0]
	publisher = &sagaCapture{}
	service = NewSaga(&repository.SagaStore{DB: db}, publisher, true)
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
	for _, result := range publisher.messages {
		if result != firstResult {
			t.Fatal("duplicate changed saved reply")
		}
	}
	wrong := m
	wrong.Data.AmountCents++
	if err = service.Handle(ctx, wrong); err == nil {
		t.Fatal("accepted payment key conflict")
	}
	m.Type = sagacontract.RefundPayment
	m.EventID = m.SagaID + ":RefundPayment"
	if err = service.Handle(ctx, m); err != nil {
		t.Fatal(err)
	}
	m.EventID += ":new-id"
	if err = service.Handle(ctx, m); err != nil {
		t.Fatal(err)
	}
	var state string
	var charges, refunds int
	if err = db.QueryRow(`SELECT state,charges,refunds FROM payments WHERE order_id=$1`, m.OrderID).Scan(&state, &charges, &refunds); err != nil {
		t.Fatal(err)
	}
	if state != "REFUNDED" || charges != 1 || refunds != 1 {
		t.Fatalf("double effect: %s %d %d", state, charges, refunds)
	}
}
