// Package sagacontract contains versioned wire DTOs, never domain entities.
package sagacontract

import (
	"errors"
	"time"
)

const (
	PaymentCommands        = "saga.payment.commands.v1"
	StockCommands          = "saga.stock.commands.v1"
	Events                 = "saga.events.v1"
	ChargePayment          = "ChargePayment"
	RefundPayment          = "RefundPayment"
	ReserveStock           = "ReserveStock"
	ReleaseStock           = "ReleaseStock"
	PaymentCharged         = "PaymentCharged"
	PaymentFailed          = "PaymentFailed"
	PaymentRefunded        = "PaymentRefunded"
	PaymentRefundFailed    = "PaymentRefundFailed"
	StockReserved          = "StockReserved"
	StockReservationFailed = "StockReservationFailed"
	StockReleased          = "StockReleased"
)

type Data struct {
	ProductID   int    `json:"product_id"`
	Quantity    int    `json:"quantity"`
	AmountCents int    `json:"amount_cents"`
	DemoFailure string `json:"demo_failure,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type Message struct {
	EventID       string    `json:"event_id"`
	SagaID        string    `json:"saga_id"`
	OrderID       string    `json:"order_id"`
	Type          string    `json:"message_type"`
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	Data          Data      `json:"data"`
}

func (m Message) Validate() error {
	if m.EventID == "" || m.SagaID == "" || m.OrderID == "" || len(m.EventID) > 150 || len(m.SagaID) > 100 || len(m.OrderID) > 100 || m.SchemaVersion != 1 || m.CreatedAt.IsZero() || m.Data.ProductID < 1 || m.Data.Quantity < 1 || m.Data.Quantity > 10 || m.Data.AmountCents <= 0 {
		return errors.New("invalid saga message envelope or operation")
	}
	if m.Data.DemoFailure != "" && m.Data.DemoFailure != "payment" && m.Data.DemoFailure != "refund" {
		return errors.New("unknown demo failure")
	}
	switch m.Type {
	case ChargePayment, RefundPayment, ReserveStock, ReleaseStock, PaymentCharged, PaymentFailed, PaymentRefunded, PaymentRefundFailed, StockReserved, StockReservationFailed, StockReleased:
		return nil
	default:
		return errors.New("unknown message type")
	}
}

func New(sagaID, orderID, kind string, data Data) Message {
	return Message{EventID: sagaID + ":" + kind, SagaID: sagaID, OrderID: orderID, Type: kind, SchemaVersion: 1, CreatedAt: time.Now().UTC(), Data: data}
}

func Reply(command Message, kind, reason string) Message {
	data := command.Data
	data.Reason = reason
	return New(command.SagaID, command.OrderID, kind, data)
}

func Topic(kind string) string {
	switch kind {
	case ChargePayment, RefundPayment:
		return PaymentCommands
	case ReserveStock, ReleaseStock:
		return StockCommands
	default:
		return Events
	}
}
