package repository

import (
	"example.com/strangler/stock-service/internal/domain"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrentReservationsNeverOversell(t *testing.T) {
	// Arrange
	store := New(domain.Product{ID: 1, Available: 7})
	var successes atomic.Int32
	var workers sync.WaitGroup
	// Act
	for i := 0; i < 60; i++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			_, err := store.Reserve(domain.Reservation{OrderID: fmt.Sprint(id), ProductID: 1, Quantity: 1})
			if err == nil {
				successes.Add(1)
			}
		}(i)
	}
	workers.Wait()
	// Assert
	product, _ := store.Get(1)
	if successes.Load() != 7 || product.Available != 0 {
		t.Fatalf("successes=%d available=%d", successes.Load(), product.Available)
	}
}

func TestRetryIsIdempotentAndRejectsChangedPayload(t *testing.T) {
	store := New(domain.Product{ID: 1, Available: 5})
	reservation := domain.Reservation{OrderID: "order-1", ProductID: 1, Quantity: 2}
	if _, err := store.Reserve(reservation); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reserve(reservation); err != nil {
		t.Fatal(err)
	}
	changed := reservation
	changed.Quantity = 1
	if _, err := store.Reserve(changed); err != domain.ErrConflict {
		t.Fatalf("expected conflict: %v", err)
	}
	product, _ := store.Get(1)
	if product.Available != 3 {
		t.Fatalf("retry deducted twice: %d", product.Available)
	}
}
