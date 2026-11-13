// Package deadletter records failed deliveries and atomically queues quarantine.
package deadletter

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"example.com/strangler/internal/bus"
	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/outbox"
	"github.com/segmentio/kafka-go"
)

//go:embed schema.sql
var Schema string

type Header struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}
type Envelope struct {
	ID              string    `json:"id"`
	SchemaVersion   int       `json:"schema_version"`
	Service         string    `json:"service"`
	ConsumerGroup   string    `json:"consumer_group"`
	SourceTopic     string    `json:"source_topic"`
	SourcePartition int       `json:"source_partition"`
	SourceOffset    int64     `json:"source_offset"`
	OriginalKey     []byte    `json:"original_key"`
	OriginalValue   []byte    `json:"original_value"`
	OriginalHeaders []Header  `json:"original_headers"`
	OriginalTime    time.Time `json:"original_time"`
	Attempts        int       `json:"attempts"`
	Error           string    `json:"error"`
	Reason          string    `json:"reason"`
	FailedAt        time.Time `json:"failed_at"`
}

type Store struct {
	DB          *sql.DB
	Outbox      *outbox.Store
	Service     string
	MaxAttempts int
}

func New(db *sql.DB, messages *outbox.Store, service string, maxAttempts int) *Store {
	if maxAttempts < 1 {
		maxAttempts = 3
	}
	return &Store{DB: db, Outbox: messages, Service: service, MaxAttempts: maxAttempts}
}

func (s *Store) IsQuarantined(ctx context.Context, m kafka.Message, group string) (bool, error) {
	var quarantined bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM consumer_failures WHERE consumer_group=$1 AND source_topic=$2 AND source_partition=$3 AND source_offset=$4 AND status='QUARANTINED')`, group, m.Topic, m.Partition, m.Offset).Scan(&quarantined)
	return quarantined, err
}

func (s *Store) OnSuccess(ctx context.Context, m kafka.Message, group string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE consumer_failures SET status='RESOLVED',updated_at=now() WHERE consumer_group=$1 AND source_topic=$2 AND source_partition=$3 AND source_offset=$4 AND status='RETRYING'`, group, m.Topic, m.Partition, m.Offset)
	return err
}

func (s *Store) OnFailure(ctx context.Context, m kafka.Message, group string, cause error) (bus.FailureDecision, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return bus.FailureDecision{}, err
	}
	defer tx.Rollback()
	var attempts int
	var status string
	err = tx.QueryRowContext(ctx, `INSERT INTO consumer_failures(consumer_group,source_topic,source_partition,source_offset,attempts,last_error,status) VALUES($1,$2,$3,$4,1,$5,'RETRYING')
 ON CONFLICT(consumer_group,source_topic,source_partition,source_offset) DO UPDATE SET attempts=CASE WHEN consumer_failures.status='QUARANTINED' THEN consumer_failures.attempts ELSE consumer_failures.attempts+1 END,
 last_error=EXCLUDED.last_error,updated_at=now() RETURNING attempts,status`, group, m.Topic, m.Partition, m.Offset, cause.Error()).Scan(&attempts, &status)
	if err != nil {
		return bus.FailureDecision{}, err
	}
	quarantine := status == "QUARANTINED" || messageerror.IsPermanent(cause) || attempts >= s.MaxAttempts
	if quarantine && status != "QUARANTINED" {
		reason := "retries_exhausted"
		if messageerror.IsPermanent(cause) {
			reason = "permanent_error"
		}
		id := fmt.Sprintf("dlq:%s:%s:%d:%d", group, m.Topic, m.Partition, m.Offset)
		headers := make([]Header, 0, len(m.Headers))
		for _, h := range m.Headers {
			headers = append(headers, Header{h.Key, h.Value})
		}
		envelope := Envelope{ID: id, SchemaVersion: 1, Service: s.Service, ConsumerGroup: group, SourceTopic: m.Topic, SourcePartition: m.Partition, SourceOffset: m.Offset, OriginalKey: m.Key, OriginalValue: m.Value, OriginalHeaders: headers, OriginalTime: m.Time, Attempts: attempts, Error: cause.Error(), Reason: reason, FailedAt: time.Now().UTC()}
		body, err := json.Marshal(envelope)
		if err != nil {
			return bus.FailureDecision{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO dead_letters(id,consumer_group,source_topic,source_partition,source_offset,envelope) VALUES($1,$2,$3,$4,$5,$6)`, id, group, m.Topic, m.Partition, m.Offset, string(body)); err != nil {
			return bus.FailureDecision{}, err
		}
		if err = s.Outbox.EnqueueJSON(ctx, tx, id, m.Topic+".dlq", string(m.Key), envelope); err != nil {
			return bus.FailureDecision{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE consumer_failures SET status='QUARANTINED' WHERE consumer_group=$1 AND source_topic=$2 AND source_partition=$3 AND source_offset=$4`, group, m.Topic, m.Partition, m.Offset); err != nil {
			return bus.FailureDecision{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return bus.FailureDecision{}, err
	}
	slog.Warn("message delivery failed", "service", s.Service, "topic", m.Topic, "partition", m.Partition, "offset", m.Offset, "group", group, "attempts", attempts, "quarantined", quarantine, "error", cause)
	return bus.FailureDecision{Quarantined: quarantine, RetryAfter: outbox.RetryDelay(attempts)}, nil
}
