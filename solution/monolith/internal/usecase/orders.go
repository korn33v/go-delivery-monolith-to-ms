package usecase

import (
	"context"
	"errors"

	"example.com/strangler/internal/contract"
	"example.com/strangler/monolith/internal/domain"
)

type LocalStock interface {
	domain.Stock
	Get(int) (domain.Product, error)
	Restore(int, int)
}

type Service struct {
	Orders    domain.Orders
	Local     LocalStock
	Remote    domain.Stock
	Publisher domain.Publisher
	Payment   domain.Payment
	Settings  domain.Settings
	Observer  domain.Observer
}

func New(orders domain.Orders, local LocalStock, remote domain.Stock, publisher domain.Publisher, payment domain.Payment, settings domain.Settings, observer domain.Observer) *Service {
	return &Service{orders, local, remote, publisher, payment, settings, observer}
}

func (s *Service) Create(ctx context.Context, request domain.CreateOrder) (domain.Order, error) {
	if err := request.Validate(); err != nil {
		return domain.Order{}, err
	}
	mode, err := s.Settings.Mode()
	if err != nil {
		return domain.Order{}, err
	}
	order := domain.Order{ID: contract.ID("mono-"), ProductID: request.ProductID, Quantity: request.Quantity, Status: "NEW", Owner: "monolith"}
	product, err := s.Local.Reserve(ctx, order.ID, request.ProductID, request.Quantity)
	if err != nil {
		return domain.Order{}, err
	}
	order.AmountCents = product.PriceCents * request.Quantity
	s.Orders.Save(order)
	if mode.Bridge {
		// Transitional bridge: keep the extracted stock synchronized while the
		// legacy writer still owns order creation. Remove after stage 4.
		_, err = s.Remote.Reserve(ctx, order.ID, request.ProductID, request.Quantity)
		if err != nil {
			if errors.Is(err, domain.ErrSoldOut) || errors.Is(err, domain.ErrNotFound) {
				s.Local.Restore(request.ProductID, request.Quantity)
				order.Status = "REJECTED"
			} else {
				order.Status = "STOCK_UNKNOWN"
			}
			_ = s.Orders.SetStatus(order.ID, order.Status)
			return order, err
		}
	}
	s.Observer.Event("order_created", "ok")
	if mode.Async {
		if err := s.Publisher.Created(ctx, order); err != nil {
			_ = s.Orders.SetStatus(order.ID, "PAYMENT_UNKNOWN")
			order.Status = "PAYMENT_UNKNOWN"
			return order, domain.ErrPublish
		}
		return order, nil
	}
	if err := s.Payment.Pay(ctx, order); err != nil {
		_ = s.Orders.SetStatus(order.ID, "PAYMENT_UNKNOWN")
		order.Status = "PAYMENT_UNKNOWN"
		return order, domain.ErrPublish
	}
	_ = s.Orders.SetStatus(order.ID, "PAID")
	order.Status = "PAID"
	s.Observer.Event("payment_local", "ok")
	return order, nil
}

func (s *Service) Get(id string) (domain.Order, error)    { return s.Orders.Get(id) }
func (s *Service) Product(id int) (domain.Product, error) { return s.Local.Get(id) }
func (s *Service) Paid(id, owner, status string) error {
	if owner != "monolith" {
		return nil
	}
	if status != "PAID" {
		return domain.ErrInvalid
	}
	err := s.Orders.SetStatus(id, status)
	// Unknown orders can be old Kafka history after an in-memory reset.
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	return err
}
