package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"

	"example.com/strangler/internal/config"
	"example.com/strangler/internal/deadletter"
	"example.com/strangler/internal/observability"
	"example.com/strangler/internal/outbox"
	"example.com/strangler/internal/postgres"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/internal/sagaruntime"
	"example.com/strangler/internal/server"
	"example.com/strangler/internal/stockclient"
	_ "example.com/strangler/stock-service/docs"
	"example.com/strangler/stock-service/internal/delivery"
	"example.com/strangler/stock-service/internal/domain"
	"example.com/strangler/stock-service/internal/repository"
	"example.com/strangler/stock-service/internal/usecase"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title GoDelivery Stock Service
// @version 1.0
// @description Изолированный склад; атомарное идемпотентное резервирование.
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
	telemetry, err := observability.New(context.Background(), "stock-service", cfg.String("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"), cfg.Bool("OTEL_ENABLED", true))
	if err != nil {
		return err
	}
	defer telemetry.Shutdown()
	if cfg.Bool("SAGA_ENABLED", false) {
		return runSaga(ctx, cfg, telemetry)
	}
	product := domain.Product{ID: 1, Name: "Ноутбук", PriceCents: 9999000, Available: cfg.Int("INITIAL_STOCK", 50)}
	if cfg.Bool("STOCK_BOOTSTRAP_FROM_MONOLITH", true) {
		// One-off data migration, NOT a read-through dependency. Pause purchases
		// until stage 2 is enabled, so the snapshot and the bridge have no gap.
		source := stockclient.Client{URL: cfg.String("MONOLITH_URL", "http://localhost:8081"), HTTP: observability.Client()}
		snapshot, err := source.Product(ctx, 1)
		if err != nil {
			return fmt.Errorf("initial stock snapshot: %w", err)
		}
		product = domain.Product(snapshot)
		slog.Info("stock snapshot imported", "available", product.Available)
	}
	store := repository.New(product)
	service := usecase.New(store)
	h := delivery.New(service)
	r := telemetry.Router("stock-service")
	r.GET("/api/v1/stock/:id", h.Get)
	r.POST("/api/v1/stock/reservations", h.Reserve)
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json")))
	return server.Run(ctx, cfg.String("STOCK_ADDR", ":8082"), r)
}

func runSaga(ctx context.Context, cfg *config.Config, t *observability.Telemetry) error {
	db, err := postgres.Open(ctx, cfg.String("DATABASE_URL", "postgres://stock:stock_demo@localhost:15432/stock?sslmode=disable"), repository.SagaSchema, outbox.Schema, deadletter.Schema)
	if err != nil {
		return err
	}
	defer db.Close()
	p, err := sagaruntime.Messaging(ctx, cfg, t, "stock-service")
	if err != nil {
		return err
	}
	defer p.Close()
	store := &repository.SagaStore{DB: db}
	h := delivery.New(usecase.New(store))
	r := t.Router("stock-service")
	r.GET("/api/v1/stock/:id", h.Get)
	r.POST("/api/v1/stock/reservations", h.Reserve)
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json")))
	messages := &outbox.Store{DB: db, Service: "stock-service"}
	kafkaHandler := &delivery.SagaHandler{Service: usecase.NewSaga(store, messages)}
	return sagaruntime.Serve(ctx, cfg, t, db, r, cfg.String("STOCK_ADDR", ":8082"), sagacontract.StockCommands, "saga-stock-v1", kafkaHandler.Command, p, messages)
}
