package usecase

import (
	"context"
	"errors"

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
	// TODO для студентов: проверить request.Validate() до любых внешних действий.
	// TODO для студентов: создать ID через contract.ID("order-") и domain.Order
	// со статусом NEW и Owner=order-service; добавить импорт internal/contract.
	// TODO для студентов: синхронно вызвать s.Stock.Reserve с этим же ctx,
	// ID заказа, ProductID и Quantity. При ошибке вернуть её без публикации.
	// TODO для студентов: вычислить AmountCents и сохранить заказ в s.Orders.
	// TODO для студентов: вызвать s.Observer.Event("order_created", "ok").
	// TODO для студентов: опубликовать через s.Publisher.Created(ctx, order).
	// Готовый Kafka adapter сохраняет OTel контекст в заголовках сообщения.
	// При ошибке публикации сохранить PAYMENT_UNKNOWN, вернуть заказ и
	// domain.ErrPublish. Не освобождать резерв при неопределённом исходе.
	// TODO для студентов: вернуть сохранённый заказ со статусом NEW.
	return domain.Order{}, domain.ErrNotImplemented
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
