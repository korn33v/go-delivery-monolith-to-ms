package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/observability"
)

// Headers adapts Kafka headers to OTel TextMapCarrier.
type Headers []kafka.Header

func (h Headers) Get(key string) string {
	for _, entry := range h {
		if strings.EqualFold(entry.Key, key) {
			return string(entry.Value)
		}
	}
	return ""
}
func (h *Headers) Set(key, value string) {
	for i := range *h {
		if strings.EqualFold((*h)[i].Key, key) {
			(*h)[i].Value = []byte(value)
			return
		}
	}
	*h = append(*h, kafka.Header{Key: key, Value: []byte(value)})
}
func (h Headers) Keys() []string {
	keys := make([]string, 0, len(h))
	for _, entry := range h {
		keys = append(keys, entry.Key)
	}
	return keys
}

type Producer struct {
	Writer    *kafka.Writer
	Telemetry *observability.Telemetry
}

func NewProducer(brokers []string, t *observability.Telemetry) *Producer {
	return &Producer{Writer: &kafka.Writer{Addr: kafka.TCP(brokers...), Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll, BatchTimeout: 10 * time.Millisecond, WriteTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, MaxAttempts: 3}, Telemetry: t}
}

func (p *Producer) Publish(ctx context.Context, topic, key string, value any) error {
	ctx, span := otel.Tracer("kafka").Start(ctx, topic+" publish", trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(attribute.String("messaging.system", "kafka"), attribute.String("messaging.destination.name", topic)))
	defer span.End()
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	headers := Headers{}
	otel.GetTextMapPropagator().Inject(ctx, &headers)
	err = p.Writer.WriteMessages(ctx, kafka.Message{Topic: topic, Key: []byte(key), Value: body, Headers: []kafka.Header(headers)})
	outcome := "ok"
	if err != nil {
		outcome = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	p.Telemetry.Kafka.WithLabelValues(topic, "produce", outcome).Inc()
	return err
}

func (p *Producer) Close() {
	if err := p.Writer.Close(); err != nil {
		slog.Error("writer close", "error", err)
	}
}

func EnsureTopics(ctx context.Context, brokers []string) error {
	dialer := &kafka.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	controller, err := conn.Controller()
	if err != nil {
		return err
	}
	admin, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return err
	}
	defer admin.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = admin.SetDeadline(deadline)
	}
	for _, topic := range []string{contract.CreatedTopic, contract.PaidTopic} {
		err := admin.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1})
		if err != nil && !errors.Is(err, kafka.TopicAlreadyExists) {
			return err
		}
	}
	return nil
}

// One message is fully processed and committed before fetching the next one.
// A failed handler retries the SAME message; later offsets cannot skip it.
func Consume(ctx context.Context, brokers []string, topic, group string, t *observability.Telemetry, handle func(context.Context, []byte) error) {
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, Topic: topic, GroupID: group, MinBytes: 1, MaxBytes: 1e6, MaxWait: time.Second, StartOffset: kafka.FirstOffset, CommitInterval: 0})
	defer reader.Close()
	for ctx.Err() == nil {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("kafka fetch", "topic", topic, "error", err)
			if !pause(ctx) {
				return
			}
			continue
		}
		for ctx.Err() == nil {
			headers := Headers(message.Headers)
			parent := otel.GetTextMapPropagator().Extract(ctx, &headers)
			work, span := otel.Tracer("kafka").Start(parent, topic+" process", trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(attribute.String("messaging.system", "kafka"), attribute.String("messaging.destination.name", topic)))
			err = handle(work, message.Value)
			outcome := "ok"
			if err != nil {
				outcome = "error"
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			t.Kafka.WithLabelValues(topic, "consume", outcome).Inc()
			slog.Info("kafka processed", "topic", topic, "group", group, "outcome", outcome, "trace_id", span.SpanContext().TraceID().String())
			span.End()
			if err == nil {
				break
			}
			// Invalid messages deliberately block this training consumer. Inspect
			// the log and reset the lab; a production consumer needs a DLQ policy.
			slog.Error("kafka handler; retrying same offset", "error", err)
			if !pause(ctx) {
				return
			}
		}
		for ctx.Err() == nil {
			if err := reader.CommitMessages(ctx, message); err == nil {
				break
			} else {
				slog.Error("kafka commit", "error", fmt.Sprint(err))
			}
			if !pause(ctx) {
				return
			}
		}
	}
}

func pause(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(time.Second):
		return true
	}
}
