package observability

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Telemetry struct {
	Registry *prometheus.Registry
	HTTP     *prometheus.CounterVec
	Duration *prometheus.HistogramVec
	Kafka    *prometheus.CounterVec
	Business *prometheus.CounterVec
	Provider *sdktrace.TracerProvider
}

func New(ctx context.Context, service, endpoint string, enabled bool) (*Telemetry, error) {
	t := &Telemetry{Registry: prometheus.NewRegistry()}
	labels := prometheus.Labels{"service": service}
	t.HTTP = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "http_requests_total", Help: "HTTP responses", ConstLabels: labels}, []string{"method", "route", "status"})
	t.Duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "http_request_duration_seconds", Help: "HTTP latency", ConstLabels: labels, Buckets: prometheus.DefBuckets}, []string{"method", "route"})
	t.Kafka = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kafka_messages_total", Help: "Kafka delivery attempts", ConstLabels: labels}, []string{"topic", "direction", "outcome"})
	t.Business = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "business_events_total", Help: "Business transitions", ConstLabels: labels}, []string{"event", "outcome"})
	t.Registry.MustRegister(t.HTTP, t.Duration, t.Kafka, t.Business, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	options := []sdktrace.TracerProviderOption{sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service))), sdktrace.WithSampler(sdktrace.AlwaysSample())}
	if enabled {
		exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
		if err != nil {
			return nil, err
		}
		options = append(options, sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(500*time.Millisecond)))
	}
	t.Provider = sdktrace.NewTracerProvider(options...)
	otel.SetTracerProvider(t.Provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return t, nil
}

func (t *Telemetry) Router(service string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), otelgin.Middleware(service), t.middleware())
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok", "service": service}) })
	r.GET("/metrics", gin.WrapH(promhttp.HandlerFor(t.Registry, promhttp.HandlerOpts{})))
	return r
}

func (t *Telemetry) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		sc := trace.SpanContextFromContext(c.Request.Context())
		c.Header("X-Trace-ID", sc.TraceID().String())
		c.Next()
		route := c.FullPath()
		if route == "/metrics" {
			return
		}
		if route == "" {
			route = "unmatched"
		}
		t.HTTP.WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		t.Duration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
		slog.Info("http", "method", c.Request.Method, "route", route, "status", c.Writer.Status(), "trace_id", sc.TraceID().String())
	}
}

func Client() *http.Client {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.ResponseHeaderTimeout = 5 * time.Second
	return &http.Client{Transport: otelhttp.NewTransport(base), Timeout: 8 * time.Second}
}

func (t *Telemetry) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := t.Provider.Shutdown(ctx); err != nil {
		slog.Error("telemetry shutdown", "error", err)
	}
}
