//go:build saga_integration

package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/internal/testdb"
)

type publisher struct {
	messages []sagacontract.Message
	fail     bool
	cancel   context.CancelFunc
}

func (p *publisher) Publish(_ context.Context, _ string, _ string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var m sagacontract.Message
	if err = json.Unmarshal(body, &m); err != nil {
		return err
	}
	p.messages = append(p.messages, m)
	if p.cancel != nil {
		p.cancel()
	}
	if p.fail {
		return errors.New("broker unavailable")
	}
	return nil
}
func operation(saga, kind string) sagacontract.Message {
	return sagacontract.New(saga, "order-"+saga, kind, sagacontract.Data{ProductID: 1, Quantity: 1, AmountCents: 100})
}
func queue(t *testing.T, s *Store, m sagacontract.Message) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = s.Enqueue(ctx, tx, m); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestRelayRetriesAfterRestartAndPreservesPerSagaOrder(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, "postgres://orders:orders_demo@localhost:15432/orders?sslmode=disable", Schema)
	s := &Store{DB: db}
	a := operation("saga-a", sagacontract.ChargePayment)
	queue(t, s, a)
	queue(t, s, operation("saga-a", sagacontract.ReserveStock))
	queue(t, s, operation("saga-b", sagacontract.ChargePayment))
	p := &publisher{fail: true}
	if worked, err := s.DispatchOne(ctx, p); !worked || err == nil {
		t.Fatal("failed publish was not persisted", worked, err)
	}
	p = &publisher{}
	if worked, err := s.DispatchOne(ctx, p); !worked || err != nil {
		t.Fatal(worked, err)
	}
	if p.messages[0].SagaID != "saga-b" {
		t.Fatal("later message overtook an unpublished predecessor", p.messages)
	}
	if _, err := db.Exec(`UPDATE outbox_messages SET next_attempt_at=now() WHERE id=$1`, a.EventID); err != nil {
		t.Fatal(err)
	}
	s = &Store{DB: db}
	for i := 0; i < 2; i++ {
		if worked, err := s.DispatchOne(ctx, p); !worked || err != nil {
			t.Fatal(worked, err)
		}
	}
	if p.messages[1] != a || p.messages[2].Type != sagacontract.ReserveStock {
		t.Fatal("recovered delivery changed payload or order", p.messages)
	}
	var pending, attempts int
	if err := db.QueryRow(`SELECT count(*) FROM outbox_messages WHERE published_at IS NULL`).Scan(&pending); err != nil || pending != 0 {
		t.Fatal(pending, err)
	}
	if err := db.QueryRow(`SELECT attempts FROM outbox_messages WHERE id=$1`, a.EventID).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatal(attempts, err)
	}
}

func TestPublishBeforeCommitCrashRedeliversSameMessage(t *testing.T) {
	db := testdb.Open(t, "postgres://orders:orders_demo@localhost:15432/orders?sslmode=disable", Schema)
	s := &Store{DB: db}
	m := operation("saga-crash", sagacontract.ChargePayment)
	queue(t, s, m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &publisher{cancel: cancel}
	if _, err := s.DispatchOne(ctx, p); err == nil {
		t.Fatal("expected interrupted local commit")
	}
	p.cancel = nil
	if worked, err := (&Store{DB: db}).DispatchOne(context.Background(), p); !worked || err != nil {
		t.Fatal(worked, err)
	}
	if len(p.messages) != 2 || p.messages[0] != p.messages[1] {
		t.Fatal("ambiguous send must redeliver the same event")
	}
}

func TestOutboxUsesCallerTransactionAndRejectsIdentityConflicts(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, "postgres://orders:orders_demo@localhost:15432/orders?sslmode=disable", Schema)
	s := &Store{DB: db}
	m := operation("saga-rollback", sagacontract.ChargePayment)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Enqueue(ctx, tx, m); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM outbox_messages`).Scan(&count); err != nil || count != 0 {
		t.Fatal("outbox escaped caller rollback", count, err)
	}
	queue(t, s, m)
	queue(t, s, m)
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	m.Data.Quantity++
	if err = s.Enqueue(ctx, tx, m); !messageerror.IsPermanent(err) {
		t.Fatal("message identity reused", err)
	}
}
