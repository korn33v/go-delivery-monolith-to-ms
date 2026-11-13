package main

import (
	"context"
	"log"
	"sync"
	"time"

	"example.com/strangler/internal/bus"
	"example.com/strangler/internal/config"
	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/observability"
	"example.com/strangler/internal/postgres"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/internal/sagaruntime"
	"example.com/strangler/internal/server"
	"example.com/strangler/payment-service/internal/delivery"
	"example.com/strangler/payment-service/internal/repository"
	"example.com/strangler/payment-service/internal/usecase"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	ctx, cancel := server.Context()
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	telemetry, err := observability.New(context.Background(), "payment-service", cfg.String("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"), cfg.Bool("OTEL_ENABLED", true))
	if err != nil {
		return err
	}
	defer telemetry.Shutdown()
	if cfg.Bool("SAGA_ENABLED", false) {
		return runSaga(ctx, cfg, telemetry)
	}
	setup, stop := context.WithTimeout(ctx, 15*time.Second)
	err = bus.EnsureTopics(setup, cfg.Brokers())
	stop()
	if err != nil {
		return err
	}
	producer := bus.NewProducer(cfg.Brokers(), telemetry)
	defer producer.Close()
	results := repository.New()
	publisher := repository.KafkaPublisher{Producer: producer}
	service := usecase.New(results, publisher, telemetry, time.Duration(cfg.Int("PAYMENT_DELAY_MS", 800))*time.Millisecond)
	h := &delivery.Handler{Service: service}
	r := telemetry.Router("payment-service")
	consumerCtx, stopConsumer := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		bus.Consume(consumerCtx, cfg.Brokers(), contract.CreatedTopic, "payments-v1", telemetry, h.Created)
	}()
	defer func() { stopConsumer(); workers.Wait() }()
	return server.Run(ctx, cfg.String("PAYMENT_ADDR", ":8084"), r)
}

func runSaga(ctx context.Context, cfg *config.Config, t *observability.Telemetry) error {
	db, err := postgres.Open(ctx, cfg.String("DATABASE_URL", "postgres://payments:payments_demo@localhost:15432/payments?sslmode=disable"), repository.SagaSchema)
	if err != nil {
		return err
	}
	defer db.Close()
	p, err := sagaruntime.Messaging(ctx, cfg, t, "payment-service")
	if err != nil {
		return err
	}
	defer p.Close()
	service := usecase.NewSaga(&repository.SagaStore{DB: db}, p, cfg.Bool("SAGA_DEMO_FAILURES", false))
	h := &delivery.SagaHandler{Service: service}
	return sagaruntime.Serve(ctx, cfg, t, db, t.Router("payment-service"), cfg.String("PAYMENT_ADDR", ":8084"), sagacontract.PaymentCommands, "saga-payment-v1", h.Command)
}
