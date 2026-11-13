package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Publisher is implemented by the existing Kafka producer and test fakes.
type Publisher interface {
	Publish(context.Context, string, string, any) error
}

func RetryDelay(attempt int) time.Duration {
	delay := 500 * time.Millisecond
	for i := 1; i < attempt && delay < 8*time.Second; i++ {
		delay *= 2
	}
	return delay
}

// DispatchOne keeps only an outbox row locked during the bounded Kafka write.
// A crash after publish but before commit causes redelivery, never lost intent.
func (s *Store) DispatchOne(ctx context.Context, publisher Publisher) (bool, error) {
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(work, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id, topic, key string
	var payload, traceJSON []byte
	var attempts int
	err = tx.QueryRowContext(work, `SELECT m.id,m.topic,m.message_key,m.payload,m.trace_context,m.attempts
 FROM outbox_messages m WHERE m.published_at IS NULL AND m.next_attempt_at<=now()
 AND NOT EXISTS(SELECT 1 FROM outbox_messages older WHERE older.message_key=m.message_key AND older.seq<m.seq AND older.published_at IS NULL)
 ORDER BY m.seq FOR UPDATE OF m SKIP LOCKED LIMIT 1`).Scan(&id, &topic, &key, &payload, &traceJSON, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var carrier propagation.MapCarrier
	if err = json.Unmarshal(traceJSON, &carrier); err != nil {
		return false, err
	}
	parent := otel.GetTextMapPropagator().Extract(work, carrier)
	// RawMessage preserves the stored JSON; []byte would be encoded as base64.
	send, stopSend := context.WithTimeout(parent, 5*time.Second)
	publishErr := publisher.Publish(send, topic, key, json.RawMessage(payload))
	stopSend()
	if publishErr == nil {
		_, err = tx.ExecContext(work, `UPDATE outbox_messages SET published_at=now(),attempts=attempts+1,last_error='' WHERE id=$1`, id)
	} else {
		_, err = tx.ExecContext(work, `UPDATE outbox_messages SET attempts=attempts+1,last_error=$2,next_attempt_at=now()+$3 * interval '1 millisecond' WHERE id=$1`, id, publishErr.Error(), RetryDelay(attempts+1).Milliseconds())
	}
	if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	var correlation struct {
		OrderID string `json:"order_id"`
		SagaID  string `json:"saga_id"`
		Type    string `json:"message_type"`
	}
	_ = json.Unmarshal(payload, &correlation)
	slog.Info("outbox delivery", "service", s.Service, "message_id", id, "topic", topic, "order_id", correlation.OrderID, "saga_id", correlation.SagaID, "message_type", correlation.Type, "saga_state", "", "attempt", attempts+1, "error", publishErr)
	return true, publishErr
}

func (s *Store) Run(ctx context.Context, publisher Publisher) {
	for ctx.Err() == nil {
		worked, err := s.DispatchOne(ctx, publisher)
		if err != nil && ctx.Err() == nil {
			slog.Error("outbox relay", "error", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}
