package repository

import (
	"context"
	"sync"

	"example.com/strangler/internal/bus"
	"example.com/strangler/internal/contract"
	"example.com/strangler/payment-service/internal/domain"
)

type Memory struct {
	mu      sync.Mutex
	results map[string]domain.Result
}

func New() *Memory { return &Memory{results: make(map[string]domain.Result)} }
func (m *Memory) Get(id string) (domain.Result, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.results[id]
	return r, ok
}
func (m *Memory) Save(id string, r domain.Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[id] = r
}

type KafkaPublisher struct{ Producer *bus.Producer }

func (p KafkaPublisher) Completed(ctx context.Context, r domain.Result) error {
	return p.Producer.Publish(ctx, contract.PaidTopic, r.OrderID, contract.PaymentCompleted{EventID: r.EventID, OrderID: r.OrderID, Owner: r.Owner, Status: r.Status})
}
