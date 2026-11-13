package domain

import "errors"

var ErrInvalid = errors.New("invalid reservation")
var ErrNotFound = errors.New("product not found")
var ErrConflict = errors.New("not enough stock or reservation key conflict")

type Product struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	PriceCents int    `json:"price_cents"`
	Available  int    `json:"available"`
}
type Reservation struct {
	OrderID   string
	ProductID int
	Quantity  int
}
type Store interface {
	Get(int) (Product, error)
	Reserve(Reservation) (Product, error)
}
