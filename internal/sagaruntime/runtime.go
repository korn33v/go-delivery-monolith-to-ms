// Package sagaruntime wires transport lifecycles, not business steps.
package sagaruntime

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"example.com/strangler/internal/bus"
	"example.com/strangler/internal/config"
	"example.com/strangler/internal/deadletter"
	"example.com/strangler/internal/observability"
	"example.com/strangler/internal/outbox"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/internal/server"
	"github.com/gin-gonic/gin"
)

func Messaging(ctx context.Context, cfg *config.Config, t *observability.Telemetry, service string) (*bus.Producer, error) {
	setup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := bus.EnsureTopics(setup, cfg.Brokers(), sagacontract.PaymentCommands, sagacontract.StockCommands, sagacontract.Events, sagacontract.PaymentCommands+".dlq", sagacontract.StockCommands+".dlq", sagacontract.Events+".dlq"); err != nil {
		return nil, err
	}
	p := bus.NewProducer(cfg.Brokers(), t)
	p.Service = service
	return p, nil
}

func Serve(ctx context.Context, cfg *config.Config, t *observability.Telemetry, db *sql.DB, r *gin.Engine, addr, topic, group string, handle func(context.Context, []byte) error, producer *bus.Producer, messages *outbox.Store) error {
	r.GET("/ready", func(c *gin.Context) {
		check, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(check); err != nil {
			c.JSON(503, gin.H{"status": "database unavailable"})
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})
	consumerCtx, stop := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Add(2)
	policy := deadletter.New(db, messages, producer.Service, cfg.Int("SAGA_MAX_ATTEMPTS", 3))
	go func() {
		defer workers.Done()
		bus.ConsumeReliable(consumerCtx, cfg.Brokers(), topic, group, t, handle, policy)
	}()
	go func() { defer workers.Done(); messages.Run(consumerCtx, producer) }()
	defer func() { stop(); workers.Wait() }()
	return server.Run(ctx, addr, r)
}
