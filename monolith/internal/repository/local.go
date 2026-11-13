package repository

import (
	"context"
	"sync"
	"time"

	"example.com/strangler/internal/config"
	"example.com/strangler/monolith/internal/domain"
	"go.opentelemetry.io/otel"
)

type MemoryStock struct {
	mu       sync.Mutex
	products map[int]domain.Product
}

func NewStock(quantity int) *MemoryStock {
	return &MemoryStock{products: map[int]domain.Product{1: {ID: 1, Name: "Ноутбук", PriceCents: 9999000, Available: quantity}}}
}
func (s *MemoryStock) Get(id int) (domain.Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.products[id]
	if !ok {
		return domain.Product{}, domain.ErrNotFound
	}
	return p, nil
}
func (s *MemoryStock) Reserve(_ context.Context, _ string, id, quantity int) (domain.Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.products[id]
	if !ok {
		return domain.Product{}, domain.ErrNotFound
	}
	if quantity <= 0 {
		return domain.Product{}, domain.ErrInvalid
	}
	if p.Available < quantity {
		return domain.Product{}, domain.ErrSoldOut
	}
	p.Available -= quantity
	s.products[id] = p
	return p, nil
}
func (s *MemoryStock) Restore(id, quantity int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.products[id]
	p.Available += quantity
	s.products[id] = p
}

type Settings struct{ Config *config.Config }

func (s Settings) Mode() (domain.Mode, error) {
	f, err := s.Config.Flags()
	return domain.Mode{Async: f.Async, Bridge: f.Bridge}, err
}

type LocalPayment struct{ Delay time.Duration }

func (p LocalPayment) Pay(ctx context.Context, order domain.Order) error {
	ctx, span := otel.Tracer("monolith").Start(ctx, "payment.local")
	defer span.End()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(p.Delay):
		return nil
	}
}
