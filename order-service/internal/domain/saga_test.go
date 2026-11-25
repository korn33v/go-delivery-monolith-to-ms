package domain

import (
	"errors"
	"testing"
)

func TestPaymentFirstSagaStartsPayment(t *testing.T) {
	s := Saga{PaymentFirst: true, State: Pending, Order: Order{Status: "PENDING"}}
	next, err := s.Start()
	if err != nil || next != "ChargePayment" || s.State != PaymentPending || s.Order.Status != "PENDING" {
		t.Fatalf("start: saga=%+v command=%q err=%v", s, next, err)
	}
}

func TestPaymentFirstSagaBusinessOutcomes(t *testing.T) {
	type event struct {
		kind, reason, nextCommand string
	}
	charged := event{kind: "PaymentCharged", nextCommand: "ReserveStock"}
	outOfStock := event{"StockReservationFailed", "insufficient_stock", "RefundPayment"}
	tests := []struct {
		name   string
		events []event
		state  SagaState
		status string
		reason string
	}{
		{"happy", []event{charged, {kind: "StockReserved"}}, Completed, "CONFIRMED", ""},
		{"payment declined", []event{{kind: "PaymentFailed", reason: "demo_payment_declined"}}, Cancelled, "CANCELLED", "demo_payment_declined"},
		{"compensated", []event{charged, outOfStock, {kind: "PaymentRefunded"}}, Cancelled, "CANCELLED", "insufficient_stock"},
		{"refund failed", []event{charged, outOfStock, {kind: "PaymentRefundFailed", reason: "demo_refund_declined"}}, CompensationFailed, "PENDING", "demo_refund_declined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Saga{PaymentFirst: true, State: Pending, Order: Order{Status: "PENDING"}}
			command, err := s.Start()
			if err != nil || command != "ChargePayment" {
				t.Fatalf("start: %q %v", command, err)
			}
			for _, e := range tt.events {
				next, err := s.Apply(e.kind, e.reason)
				if err != nil || next != e.nextCommand {
					t.Fatalf("%s: want command %q, got %q (err=%v)", e.kind, e.nextCommand, next, err)
				}
			}
			if s.State != tt.state || s.Order.Status != tt.status || s.Reason != tt.reason {
				t.Fatalf("outcome: %+v", s)
			}
		})
	}
}

func TestPaymentFirstSagaCommandsAndCompensationPreconditions(t *testing.T) {
	s := Saga{PaymentFirst: true, State: PaymentPending, Order: Order{Status: "PENDING"}}
	if next, err := s.Apply("PaymentCharged", ""); err != nil || next != "ReserveStock" || s.State != StockPending || s.Order.Status != "PENDING" {
		t.Fatalf("reserve: %+v %q %v", s, next, err)
	}
	if next, err := s.Apply("StockReservationFailed", "insufficient_stock"); err != nil || next != "RefundPayment" || s.State != Compensating {
		t.Fatalf("refund: %+v %q %v", s, next, err)
	}
	if s.Order.Status != "PENDING" || s.Reason != "insufficient_stock" {
		t.Fatalf("awaiting refund: %+v", s)
	}
}

func TestPaymentFirstSagaRejectsInvalidAndDuplicateTransitions(t *testing.T) {
	states := []SagaState{Pending, PaymentPending, StockPending, Compensating, Completed, Cancelled, CompensationFailed}
	events := []string{"PaymentCharged", "PaymentFailed", "StockReserved", "StockReservationFailed", "PaymentRefunded", "PaymentRefundFailed", "Unknown"}
	allowed := map[SagaState]map[string]bool{PaymentPending: {"PaymentCharged": true, "PaymentFailed": true}, StockPending: {"StockReserved": true, "StockReservationFailed": true}, Compensating: {"PaymentRefunded": true, "PaymentRefundFailed": true}}
	for _, state := range states {
		for _, event := range events {
			if allowed[state][event] {
				continue
			}
			s := Saga{PaymentFirst: true, State: state, Order: Order{Status: "PENDING"}, Reason: "previous_reason"}
			before := s
			if _, err := s.Apply(event, ""); !errors.Is(err, ErrTransition) || s != before {
				t.Fatalf("%s + %s: want ErrTransition and unchanged saga; got %v, before=%+v after=%+v", state, event, err, before, s)
			}
		}
	}
	s := Saga{PaymentFirst: true, State: PaymentPending}
	if _, err := s.Apply("PaymentCharged", ""); err != nil {
		t.Fatal(err)
	}
	before := s
	if _, err := s.Apply("PaymentCharged", ""); !errors.Is(err, ErrTransition) || s != before {
		t.Fatal("duplicate must be deduplicated before applying a transition")
	}
}

func TestSagaRejectsRestart(t *testing.T) {
	for _, tc := range []struct {
		state       SagaState
		orderStatus string
	}{
		{PaymentPending, "PENDING"},
		{StockPending, "PENDING"},
		{Compensating, "PENDING"},
		{Completed, "CONFIRMED"},
		{Cancelled, "CANCELLED"},
		{CompensationFailed, "PENDING"},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			s := Saga{State: tc.state, Reason: "previous_reason", Order: Order{Status: tc.orderStatus}}
			before := s
			if _, err := s.Start(); !errors.Is(err, ErrTransition) || s != before {
				t.Fatalf("restart: want ErrTransition and unchanged saga; got %v, before=%+v after=%+v", err, before, s)
			}
		})
	}
}
