// Package contract contains wire formats, not a shared service database.
package contract

import (
	"crypto/rand"
	"encoding/hex"
)

const CreatedTopic = "order-created"
const PaidTopic = "payment-completed"

type Product struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	PriceCents int    `json:"price_cents"`
	Available  int    `json:"available"`
}

type CreateOrder struct {
	ProductID int `json:"product_id" example:"1"`
	Quantity  int `json:"quantity" example:"1"`
}

type Order struct {
	ID          string `json:"id"`
	ProductID   int    `json:"product_id"`
	Quantity    int    `json:"quantity"`
	AmountCents int    `json:"amount_cents"`
	Status      string `json:"status"`
	Owner       string `json:"owner"`
}

type Reservation struct {
	OrderID   string `json:"order_id"`
	ProductID int    `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type OrderCreated struct {
	EventID     string `json:"event_id"`
	OrderID     string `json:"order_id"`
	ProductID   int    `json:"product_id"`
	Quantity    int    `json:"quantity"`
	AmountCents int    `json:"amount_cents"`
	Owner       string `json:"owner"`
}

type PaymentCompleted struct {
	EventID string `json:"event_id"`
	OrderID string `json:"order_id"`
	Owner   string `json:"owner"`
	Status  string `json:"status"`
}

type APIError struct {
	Error string `json:"error"`
	Order *Order `json:"order,omitempty"`
}

func ID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}
