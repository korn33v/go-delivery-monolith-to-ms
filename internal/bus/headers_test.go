package bus

import (
	"context"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"testing"
)

func TestKafkaHeadersPreserveRemoteParent(t *testing.T) {
	id, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	spanID, _ := trace.SpanIDFromHex("0123456789abcdef")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: id, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	carrier := Headers{}
	propagator := propagation.TraceContext{}
	propagator.Inject(ctx, &carrier)
	extracted := propagator.Extract(context.Background(), &carrier)
	parent := trace.SpanContextFromContext(extracted)
	if parent.TraceID() != id || parent.SpanID() != spanID || !parent.IsRemote() {
		t.Fatal("Kafka context propagation lost the parent")
	}
	carrier.Set("traceparent", carrier.Get("traceparent"))
	if len(carrier) != 1 {
		t.Fatal("duplicate traceparent header")
	}
}
