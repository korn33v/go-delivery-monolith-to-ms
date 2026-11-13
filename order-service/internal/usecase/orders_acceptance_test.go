//go:build acceptance

package usecase

import (
	"context"
	"errors"
	"example.com/strangler/order-service/internal/domain"
	"example.com/strangler/order-service/internal/repository"
	"testing"
)

type fakeStock struct {
	calls int
	fail  error
}

func (s *fakeStock) Reserve(ctx context.Context, id string, productID, quantity int) (domain.Product, error) {
	s.calls++
	return domain.Product{ID: productID, PriceCents: 100}, s.fail
}

type fakePublisher struct {
	orders []domain.Order
	fail   error
}

func (p *fakePublisher) Created(ctx context.Context, order domain.Order) error {
	p.orders = append(p.orders, order)
	return p.fail
}

type observer struct{}

func (observer) Event(string, string) {}

func TestCreateValidateReservePersistPublish(t *testing.T) {
	// Arrange
	orders := repository.NewOrders()
	stock := &fakeStock{}
	publisher := &fakePublisher{}
	service := New(orders, stock, publisher, observer{})
	// Act
	order, err := service.Create(context.Background(), domain.CreateOrder{ProductID: 1, Quantity: 2})
	// Assert
	if err != nil {
		t.Fatalf("complete student TODO blocks: %v", err)
	}
	saved, err := orders.Get(order.ID)
	if err != nil || saved.Status != "NEW" || saved.AmountCents != 200 || stock.calls != 1 || len(publisher.orders) != 1 {
		t.Fatalf("invalid workflow: %+v, stock=%d, published=%d", saved, stock.calls, len(publisher.orders))
	}
	if err := service.Paid(order.ID, "order-service", "PAID"); err != nil {
		t.Fatal(err)
	}
	if err := service.Paid(order.ID, "order-service", "PAID"); err != nil {
		t.Fatal(err)
	}
	paid, _ := orders.Get(order.ID)
	if paid.Status != "PAID" {
		t.Fatal("result event was not applied")
	}
}
func TestInvalidRequestDoesNotTouchStock(t *testing.T) {
	stock := &fakeStock{}
	publisher := &fakePublisher{}
	service := New(repository.NewOrders(), stock, publisher, observer{})
	_, err := service.Create(context.Background(), domain.CreateOrder{ProductID: 1, Quantity: 0})
	if !errors.Is(err, domain.ErrInvalid) || stock.calls != 0 || len(publisher.orders) != 0 {
		t.Fatalf("invalid request produced side effects: %v", err)
	}
}
func TestPublicationTimeoutKeepsOrderForReconciliation(t *testing.T) {
	orders := repository.NewOrders()
	publisher := &fakePublisher{fail: context.DeadlineExceeded}
	service := New(orders, &fakeStock{}, publisher, observer{})
	order, err := service.Create(context.Background(), domain.CreateOrder{ProductID: 1, Quantity: 1})
	if !errors.Is(err, domain.ErrPublish) {
		t.Fatalf("expected uncertain publication: %v", err)
	}
	saved, getErr := orders.Get(order.ID)
	if getErr != nil || saved.Status != "PAYMENT_UNKNOWN" {
		t.Fatalf("lost uncertain order: %+v %v", saved, getErr)
	}
}
