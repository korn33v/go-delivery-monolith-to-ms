//go:build !saga_assignment

package domain

import (
	"errors"
	"testing"
)

func TestSagaStartsStock(t *testing.T) {
	s := Saga{State: Pending, Order: Order{Status: "PENDING"}}
	next, err := s.Start()
	if err != nil || next != "ReserveStock" || s.State != StockPending || s.Order.Status != "PENDING" {
		t.Fatalf("start: saga=%+v command=%q err=%v", s, next, err)
	}
}

func TestSagaBusinessOutcomes(t *testing.T) {
	type event struct {
		kind, reason, nextCommand string
	}
	reserved := event{kind: "StockReserved", nextCommand: "ChargePayment"}
	outOfStock := event{kind: "StockReservationFailed", reason: "insufficient_stock"}
	tests := []struct {
		name   string
		events []event
		state  SagaState
		status string
		reason string
	}{
		{"happy", []event{reserved, {kind: "PaymentCharged"}}, Completed, "CONFIRMED", ""},
		{"out of stock", []event{outOfStock}, Cancelled, "CANCELLED", "insufficient_stock"},
		{"payment declined", []event{reserved, {kind: "PaymentFailed", reason: "demo_payment_declined", nextCommand: "ReleaseStock"}, {kind: "StockReleased"}}, Cancelled, "CANCELLED", "demo_payment_declined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Saga{State: Pending, Order: Order{Status: "PENDING"}}
			command, err := s.Start()
			if err != nil || command != "ReserveStock" {
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

func TestSagaCommandsAndCompensationPreconditions(t *testing.T) {
	s := Saga{State: StockPending, Order: Order{Status: "PENDING"}}
	if next, err := s.Apply("StockReserved", ""); err != nil || next != "ChargePayment" || s.State != PaymentPending || s.Order.Status != "PENDING" {
		t.Fatalf("payment: %+v %q %v", s, next, err)
	}
	if next, err := s.Apply("PaymentFailed", "demo_payment_declined"); err != nil || next != "ReleaseStock" || s.State != Compensating {
		t.Fatalf("release: %+v %q %v", s, next, err)
	}
	if s.Order.Status != "PENDING" || s.Reason != "demo_payment_declined" {
		t.Fatalf("awaiting release: %+v", s)
	}
}

func TestSagaRejectsInvalidAndDuplicateTransitions(t *testing.T) {
	states := []SagaState{Pending, PaymentPending, StockPending, Compensating, Completed, Cancelled, CompensationFailed}
	events := []string{"PaymentCharged", "PaymentFailed", "StockReserved", "StockReservationFailed", "StockReleased", "PaymentRefunded", "PaymentRefundFailed", "Unknown"}
	allowed := map[SagaState]map[string]bool{PaymentPending: {"PaymentCharged": true, "PaymentFailed": true}, StockPending: {"StockReserved": true, "StockReservationFailed": true}, Compensating: {"StockReleased": true}}
	for _, state := range states {
		for _, event := range events {
			if allowed[state][event] {
				continue
			}
			s := Saga{State: state, Order: Order{Status: "PENDING"}, Reason: "previous_reason"}
			before := s
			if _, err := s.Apply(event, ""); !errors.Is(err, ErrTransition) || s != before {
				t.Fatalf("%s + %s: want ErrTransition and unchanged saga; got %v, before=%+v after=%+v", state, event, err, before, s)
			}
		}
	}
	s := Saga{State: PaymentPending}
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
