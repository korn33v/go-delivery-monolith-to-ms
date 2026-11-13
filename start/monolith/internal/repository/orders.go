package repository

import (
	"example.com/strangler/monolith/internal/domain"
	"sync"
)

// This map belongs to this process. No other service can access it.
type MemoryOrders struct {
	mu     sync.Mutex
	orders map[string]domain.Order
}

func NewOrders() *MemoryOrders { return &MemoryOrders{orders: make(map[string]domain.Order)} }
func (r *MemoryOrders) Save(order domain.Order) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.orders[order.ID] = order
}
func (r *MemoryOrders) Get(id string) (domain.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, ok := r.orders[id]
	if !ok {
		return domain.Order{}, domain.ErrNotFound
	}
	return order, nil
}
func (r *MemoryOrders) SetStatus(id, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, ok := r.orders[id]
	if !ok {
		return domain.ErrNotFound
	}
	// Late duplicate events cannot regress a paid order.
	if order.Status == "PAID" {
		return nil
	}
	order.Status = status
	r.orders[id] = order
	return nil
}
