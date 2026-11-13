package domain

import (
	"context"
	"errors"
)

var ErrInvalid = errors.New("product_id must be positive; quantity must be between 1 and 10")
var ErrNotFound = errors.New("not found")
var ErrSoldOut = errors.New("not enough stock")
var ErrStock = errors.New("stock service unavailable; reservation may need reconciliation")
var ErrPublish = errors.New("payment publication uncertain; inspect this order before retrying")
var ErrNotImplemented = errors.New("student TODO: implement order handler and use case")

type CreateOrder struct {
	ProductID int
	Quantity  int
}

func (r CreateOrder) Validate() error {
	if r.ProductID < 1 || r.Quantity < 1 || r.Quantity > 10 {
		return ErrInvalid
	}
	return nil
}

type Order struct {
	ID          string `json:"id"`
	ProductID   int    `json:"product_id"`
	Quantity    int    `json:"quantity"`
	AmountCents int    `json:"amount_cents"`
	Status      string `json:"status"`
	Owner       string `json:"owner"`
}

type Product struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	PriceCents int    `json:"price_cents"`
	Available  int    `json:"available"`
}

type Orders interface {
	Save(Order)
	Get(string) (Order, error)
	SetStatus(string, string) error
}

type Stock interface {
	Reserve(context.Context, string, int, int) (Product, error)
}

type Publisher interface {
	Created(context.Context, Order) error
}
type Observer interface{ Event(string, string) }
type Mode struct {
	Async  bool
	Bridge bool
}
type Settings interface{ Mode() (Mode, error) }
type Payment interface {
	Pay(context.Context, Order) error
}
