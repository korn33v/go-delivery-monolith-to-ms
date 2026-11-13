package usecase

import (
	"context"
	"errors"
	"example.com/strangler/payment-service/internal/domain"
	"time"
)

type Service struct {
	Results   domain.Results
	Publisher domain.Publisher
	Observer  domain.Observer
	Delay     time.Duration
}

func New(results domain.Results, publisher domain.Publisher, observer domain.Observer, delay time.Duration) *Service {
	return &Service{results, publisher, observer, delay}
}

// Consume is sequential in this lab. The memory cache suppresses repeat charges
// within this process, while a repeated result can safely be published again.
func (s *Service) Pay(ctx context.Context, order domain.Order) error {
	if order.ID == "" || order.AmountCents <= 0 || order.Quantity <= 0 || (order.Owner != "monolith" && order.Owner != "order-service") {
		return errors.New("invalid order-created event")
	}
	result, ok := s.Results.Get(order.ID)
	if !ok {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.Delay):
		}
		result = domain.Result{EventID: order.ID + "-paid", OrderID: order.ID, Owner: order.Owner, Status: "PAID"}
		s.Results.Save(order.ID, result)
		s.Observer.Event("payment_processed", "ok")
	}
	return s.Publisher.Completed(ctx, result)
}
