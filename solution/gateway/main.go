package main

import (
	"context"
	"log"

	_ "example.com/strangler/gateway/docs"
	"example.com/strangler/gateway/router"
	"example.com/strangler/internal/config"
	"example.com/strangler/internal/httpapi"
	"example.com/strangler/internal/observability"
	"example.com/strangler/internal/server"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title GoDelivery Strangler Gateway
// @version 1.0
// @description Переключение HTTP-маршрутов между монолитом и сервисами.
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
	telemetry, err := observability.New(context.Background(), "gateway", cfg.String("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"), cfg.Bool("OTEL_ENABLED", true))
	if err != nil {
		return err
	}
	defer telemetry.Shutdown()
	gateway, err := router.New(cfg)
	if err != nil {
		return err
	}
	r := telemetry.Router("gateway")
	r.Use(httpapi.CORS(cfg.String("FRONTEND_ORIGIN", "http://localhost:5173")))
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json")))
	r.GET("/api/v1/stock/:id", gateway.StockRead)
	r.POST("/api/v1/orders", gateway.CreateOrder)
	r.GET("/api/v1/orders/:id", gateway.GetOrder)
	r.GET("/api/v1/migration", gateway.Migration)
	return server.Run(ctx, cfg.String("GATEWAY_ADDR", ":8080"), r)
}
