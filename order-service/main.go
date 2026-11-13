package main

import (
	"context"
	"log"
	"sync"

	"example.com/strangler/internal/bus"
	"example.com/strangler/internal/config"
	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/deadletter"
	"example.com/strangler/internal/observability"
	"example.com/strangler/internal/outbox"
	"example.com/strangler/internal/postgres"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/internal/sagaruntime"
	"example.com/strangler/internal/server"
	"example.com/strangler/internal/stockclient"
	_ "example.com/strangler/order-service/docs"
	"example.com/strangler/order-service/handler"
	"example.com/strangler/order-service/internal/repository"
	"example.com/strangler/order-service/internal/usecase"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"time"
)

// @title GoDelivery order-service
// @version 1.0
// @description Создание и чтение заказов, собственное хранилище, Kafka.
// @BasePath /
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
	telemetry, err := observability.New(context.Background(), "order-service", cfg.String("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"), cfg.Bool("OTEL_ENABLED", true))
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
	orders := repository.NewOrders()
	publisher := repository.KafkaPublisher{Producer: producer}

	stock := repository.RemoteStock{Client: &stockclient.Client{URL: cfg.String("STOCK_URL", "http://localhost:8082"), HTTP: observability.Client()}}
	service := usecase.New(orders, stock, publisher, telemetry)

	h := handler.New(service)
	r := telemetry.Router("order-service")
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json")))
	h.Register(r)

	// Stop and join the consumer before closing its producer and trace exporter.
	consumerCtx, stopConsumer := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		bus.Consume(consumerCtx, cfg.Brokers(), contract.PaidTopic, "order-service-payment-results-v1", telemetry, h.PaymentResult)
	}()
	defer func() { stopConsumer(); workers.Wait() }()
	return server.Run(ctx, cfg.String("ORDER_ADDR", ":8083"), r)
}

func runSaga(ctx context.Context, cfg *config.Config, t *observability.Telemetry) error {
	db, err := postgres.Open(ctx, cfg.String("DATABASE_URL", "postgres://orders:orders_demo@localhost:15432/orders?sslmode=disable"), repository.SagaSchema, outbox.Schema, deadletter.Schema)
	if err != nil {
		return err
	}
	defer db.Close()
	p, err := sagaruntime.Messaging(ctx, cfg, t, "order-service")
	if err != nil {
		return err
	}
	defer p.Close()
	messages := &outbox.Store{DB: db, Service: "order-service"}
	service := usecase.NewOrchestrator(&repository.SagaStore{DB: db}, messages, cfg.Bool("SAGA_DEMO_FAILURES", false))
	h := &handler.SagaHandler{Service: service}
	r := t.Router("order-service")
	r.POST("/api/v1/orders", h.Create)
	r.GET("/api/v1/orders/:id", h.Get)
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json")))
	return sagaruntime.Serve(ctx, cfg, t, db, r, cfg.String("ORDER_ADDR", ":8083"), sagacontract.Events, "saga-order-v1", h.Event, p, messages)
}
