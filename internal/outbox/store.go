// Package outbox stores outgoing messages in the caller's local transaction.
package outbox

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"

	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/sagacontract"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

//go:embed schema.sql
var Schema string

type Store struct {
	DB      *sql.DB
	Service string
}

func (s *Store) Enqueue(ctx context.Context, tx *sql.Tx, m sagacontract.Message) error {
	if err := m.Validate(); err != nil {
		return err
	}
	return s.EnqueueJSON(ctx, tx, m.EventID, sagacontract.Topic(m.Type), m.SagaID, m)
}

func (s *Store) EnqueueJSON(ctx context.Context, tx *sql.Tx, id, topic, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	traceJSON, err := json.Marshal(carrier)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO outbox_messages(id,topic,message_key,payload,trace_context) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO NOTHING`, id, topic, key, string(payload), string(traceJSON))
	if err != nil {
		return err
	}
	added, err := result.RowsAffected()
	if err != nil || added == 1 {
		return err
	}
	// A repeated decision is safe; reusing its identity for other data is not.
	var same bool
	if err = tx.QueryRowContext(ctx, `SELECT topic=$2 AND message_key=$3 AND payload=$4::jsonb FROM outbox_messages WHERE id=$1`, id, topic, key, string(payload)).Scan(&same); err != nil {
		return err
	}
	if !same {
		return messageerror.Permanent(errors.New("outbox message identity conflict"))
	}
	return nil
}
