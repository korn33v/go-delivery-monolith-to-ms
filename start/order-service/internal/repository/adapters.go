package repository

import (
	"context"
	"errors"
	"net/http"

	"example.com/strangler/internal/bus"
	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/stockclient"
	"example.com/strangler/order-service/internal/domain"
)

type KafkaPublisher struct{ Producer *bus.Producer }

func (p KafkaPublisher) Created(ctx context.Context, order domain.Order) error {
	return p.Producer.Publish(ctx, contract.CreatedTopic, order.ID, contract.OrderCreated{
		EventID: order.ID + "-created", OrderID: order.ID, ProductID: order.ProductID,
		Quantity: order.Quantity, AmountCents: order.AmountCents, Owner: order.Owner,
	})
}

type RemoteStock struct{ Client *stockclient.Client }

func (s RemoteStock) Reserve(ctx context.Context, id string, productID, quantity int) (domain.Product, error) {
	product, err := s.Client.Reserve(ctx, contract.Reservation{OrderID: id, ProductID: productID, Quantity: quantity})
	if err != nil {
		var remote *stockclient.HTTPError
		if errors.As(err, &remote) {
			if remote.Status == http.StatusConflict {
				return domain.Product{}, domain.ErrSoldOut
			}
			if remote.Status == http.StatusNotFound {
				return domain.Product{}, domain.ErrNotFound
			}
		}
		return domain.Product{}, domain.ErrStock
	}
	return domain.Product(product), nil
}
