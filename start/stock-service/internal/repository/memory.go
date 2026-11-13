package repository

import (
	"example.com/strangler/stock-service/internal/domain"
	"sync"
)

type Memory struct {
	mu           sync.Mutex
	products     map[int]domain.Product
	reservations map[string]domain.Reservation
}

func New(product domain.Product) *Memory {
	return &Memory{products: map[int]domain.Product{product.ID: product}, reservations: make(map[string]domain.Reservation)}
}
func (m *Memory) Get(id int) (domain.Product, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.products[id]
	if !ok {
		return domain.Product{}, domain.ErrNotFound
	}
	return p, nil
}
func (m *Memory) Reserve(r domain.Reservation) (domain.Product, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.products[r.ProductID]
	if !ok {
		return domain.Product{}, domain.ErrNotFound
	}
	if previous, exists := m.reservations[r.OrderID]; exists {
		if previous != r {
			return domain.Product{}, domain.ErrConflict
		}
		return p, nil
	}
	if r.Quantity <= 0 || r.OrderID == "" {
		return domain.Product{}, domain.ErrInvalid
	}
	if p.Available < r.Quantity {
		return domain.Product{}, domain.ErrConflict
	}
	p.Available -= r.Quantity
	m.products[r.ProductID] = p
	m.reservations[r.OrderID] = r
	return p, nil
}
