//go:build saga_integration

package deadletter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/outbox"
	"example.com/strangler/internal/testdb"
	"github.com/segmentio/kafka-go"
)

func TestPermanentFailureQuarantinesOriginalBytesOnce(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, "postgres://payments:payments_demo@localhost:15432/payments?sslmode=disable", outbox.Schema, Schema)
	s := New(db, &outbox.Store{DB: db}, "payment-service", 3)
	m := kafka.Message{Topic: "saga.payment.commands.v1", Partition: 0, Offset: 42, Key: []byte("saga-x"), Value: []byte{0xff, '{', '?'}, Headers: []kafka.Header{{Key: "binary", Value: []byte{0, 255}}}}
	cause := messageerror.Permanent(errors.New("invalid JSON"))
	for i := 0; i < 2; i++ {
		decision, err := s.OnFailure(ctx, m, "payments", cause)
		if err != nil || !decision.Quarantined {
			t.Fatal(decision, err)
		}
	}
	if quarantined, err := New(db, &outbox.Store{DB: db}, "payment-service", 3).IsQuarantined(ctx, m, "payments"); err != nil || !quarantined {
		t.Fatal("quarantine lost after restart", quarantined, err)
	}
	var data []byte
	var count int
	if err := db.QueryRow(`SELECT envelope FROM dead_letters`).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(envelope.OriginalValue, m.Value) || !bytes.Equal(envelope.OriginalHeaders[0].Value, m.Headers[0].Value) || envelope.Attempts != 1 || envelope.Reason != "permanent_error" {
		t.Fatal("lost DLQ metadata", envelope)
	}
	if err := db.QueryRow(`SELECT count(*) FROM outbox_messages WHERE topic='saga.payment.commands.v1.dlq'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate DLQ intent", count, err)
	}
}

func TestRetryBudgetSurvivesRestartAndSuccessResolvesFailure(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, "postgres://payments:payments_demo@localhost:15432/payments?sslmode=disable", outbox.Schema, Schema)
	m := kafka.Message{Topic: "saga.payment.commands.v1", Partition: 1, Offset: 100, Value: []byte(`{}`)}
	for i := 1; i <= 3; i++ {
		s := New(db, &outbox.Store{DB: db}, "payment-service", 3)
		decision, err := s.OnFailure(ctx, m, "payments", errors.New("temporary database operation failed"))
		if err != nil || decision.Quarantined != (i == 3) {
			t.Fatal(i, decision, err)
		}
	}
	var attempts int
	if err := db.QueryRow(`SELECT (envelope->>'attempts')::int FROM dead_letters`).Scan(&attempts); err != nil || attempts != 3 {
		t.Fatal(attempts, err)
	}
	m.Offset++
	s := New(db, &outbox.Store{DB: db}, "payment-service", 3)
	if _, err := s.OnFailure(ctx, m, "payments", errors.New("transient")); err != nil {
		t.Fatal(err)
	}
	if err := s.OnSuccess(ctx, m, "payments"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM consumer_failures WHERE source_offset=$1`, m.Offset).Scan(&status); err != nil || status != "RESOLVED" {
		t.Fatal(status, err)
	}
}

func TestCannotAcknowledgeFailureWithoutDurableDlqIntent(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t, "postgres://payments:payments_demo@localhost:15432/payments?sslmode=disable", outbox.Schema, Schema)
	if _, err := db.Exec(`DROP TABLE outbox_messages`); err != nil {
		t.Fatal(err)
	}
	s := New(db, &outbox.Store{DB: db}, "payment-service", 3)
	decision, err := s.OnFailure(ctx, kafka.Message{Topic: "commands", Offset: 9}, "payments", messageerror.Permanent(errors.New("bad command")))
	if err == nil || decision.Quarantined {
		t.Fatal("failure was acknowledged without queued DLQ", decision, err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM dead_letters`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial quarantine commit", count, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM consumer_failures`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial attempt commit", count, err)
	}
}
