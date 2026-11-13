package main

import (
	"context"
	"log"
	"sync"

	"example.com/strangler/monolith/internal/domain"

	"example.com/strangler/internal/bus"
	"example.com/strangler/internal/config"
	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/observability"
	"example.com/strangler/internal/server"
	"example.com/strangler/internal/stockclient"
	_ "example.com/strangler/monolith/docs"
	"example.com/strangler/monolith/internal/delivery"
	"example.com/strangler/monolith/internal/repository"
	"example.com/strangler/monolith/internal/usecase"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"time"
)

// @title GoDelivery monolith
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
	telemetry, err := observability.New(context.Background(), "monolith", cfg.String("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"), cfg.Bool("OTEL_ENABLED", true))
	if err != nil {
		return err
	}
	defer telemetry.Shutdown()
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

	local := repository.NewStock(cfg.Int("INITIAL_STOCK", 50))
	remote := repository.RemoteStock{Client: &stockclient.Client{URL: cfg.String("STOCK_URL", "http://localhost:8082"), HTTP: observability.Client()}}
	payment := repository.LocalPayment{Delay: time.Duration(cfg.Int("PAYMENT_DELAY_MS", 800)) * time.Millisecond}
	settings := repository.Settings{Config: cfg}
	var _ domain.Orders = orders
	service := usecase.New(orders, local, remote, publisher, payment, settings, telemetry)

	h := delivery.New(service)
	r := telemetry.Router("monolith")
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json")))
	h.Register(r)
	r.GET("/api/v1/stock/:id", h.Product)
	// Stop and join the consumer before closing its producer and trace exporter.
	consumerCtx, stopConsumer := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		bus.Consume(consumerCtx, cfg.Brokers(), contract.PaidTopic, "monolith-payment-results-v1", telemetry, h.PaymentResult)
	}()
	defer func() { stopConsumer(); workers.Wait() }()
	return server.Run(ctx, cfg.String("MONOLITH_ADDR", ":8081"), r)
}
