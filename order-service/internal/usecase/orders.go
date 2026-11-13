package usecase

import (
	"context"
	"errors"

	"example.com/strangler/internal/contract"
	"example.com/strangler/order-service/internal/domain"
)

type Service struct {
	Orders    domain.Orders
	Stock     domain.Stock
	Publisher domain.Publisher
	Observer  domain.Observer
}

func New(orders domain.Orders, stock domain.Stock, publisher domain.Publisher, observer domain.Observer) *Service {
	return &Service{orders, stock, publisher, observer}
}

func (s *Service) Create(ctx context.Context, request domain.CreateOrder) (domain.Order, error) {
	if err := request.Validate(); err != nil {
		return domain.Order{}, err
	}
	order := domain.Order{ID: contract.ID("order-"), ProductID: request.ProductID, Quantity: request.Quantity, Status: "NEW", Owner: "order-service"}
	product, err := s.Stock.Reserve(ctx, order.ID, request.ProductID, request.Quantity)
	if err != nil {
		return domain.Order{}, err
	}
	order.AmountCents = product.PriceCents * request.Quantity
	s.Orders.Save(order)
	s.Observer.Event("order_created", "ok")
	if err := s.Publisher.Created(ctx, order); err != nil {
		// A timeout does not prove that Kafka rejected the message. Keep the
		// order and its reservation; do not invent a distributed rollback.
		_ = s.Orders.SetStatus(order.ID, "PAYMENT_UNKNOWN")
		order.Status = "PAYMENT_UNKNOWN"
		return order, domain.ErrPublish
	}
	return order, nil
}

func (s *Service) Get(id string) (domain.Order, error) { return s.Orders.Get(id) }
func (s *Service) Paid(id, owner, status string) error {
	if owner != "order-service" {
		return nil
	}
	if status != "PAID" {
		return domain.ErrInvalid
	}
	err := s.Orders.SetStatus(id, status)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	return err
}
