package domain

import (
	"errors"
	"testing"
)

func TestSagaBusinessOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		events []string
		state  SagaState
		status string
	}{
		{"happy", []string{"PaymentCharged", "StockReserved"}, Completed, "CONFIRMED"},
		{"payment declined", []string{"PaymentFailed"}, Cancelled, "CANCELLED"},
		{"compensated", []string{"PaymentCharged", "StockReservationFailed", "PaymentRefunded"}, Cancelled, "CANCELLED"},
		{"refund failed", []string{"PaymentCharged", "StockReservationFailed", "PaymentRefundFailed"}, CompensationFailed, "PENDING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Saga{State: Pending, Order: Order{Status: "PENDING"}}
			command, err := s.Start()
			if err != nil || command != "ChargePayment" {
				t.Fatalf("start: %q %v", command, err)
			}
			for _, event := range tt.events {
				if _, err = s.Apply(event, "business_failure"); err != nil {
					t.Fatal(err)
				}
			}
			if s.State != tt.state || s.Order.Status != tt.status {
				t.Fatalf("outcome: %+v", s)
			}
		})
	}
}

func TestSagaCommandsAndCompensationPreconditions(t *testing.T) {
	s := Saga{State: PaymentPending, Order: Order{Status: "PENDING"}}
	if next, err := s.Apply("PaymentCharged", ""); err != nil || next != "ReserveStock" || s.State != StockPending {
		t.Fatalf("reserve: %+v %q %v", s, next, err)
	}
	if next, err := s.Apply("StockReservationFailed", "insufficient_stock"); err != nil || next != "RefundPayment" || s.State != Compensating {
		t.Fatalf("refund: %+v %q %v", s, next, err)
	}
	if s.Order.Status != "PENDING" {
		t.Fatal("must await confirmed refund")
	}
	for _, tc := range []struct {
		event, reason, wantReason, wantStatus string
		wantState                             SagaState
	}{
		{"PaymentRefunded", "", "insufficient_stock", "CANCELLED", Cancelled},
		{"PaymentRefundFailed", "demo_refund_declined", "demo_refund_declined", "PENDING", CompensationFailed},
	} {
		t.Run(tc.event, func(t *testing.T) {
			waiting := s
			next, err := waiting.Apply(tc.event, tc.reason)
			if err != nil || next != "" || waiting.State != tc.wantState ||
				waiting.Order.Status != tc.wantStatus || waiting.Reason != tc.wantReason {
				t.Fatalf("after %s: %+v next=%q err=%v", tc.event, waiting, next, err)
			}
		})
	}
}

func TestSagaRejectsInvalidAndDuplicateTransitions(t *testing.T) {
	states := []SagaState{Pending, PaymentPending, StockPending, Compensating, Completed, Cancelled, CompensationFailed}
	events := []string{"PaymentCharged", "PaymentFailed", "StockReserved", "StockReservationFailed", "PaymentRefunded", "PaymentRefundFailed", "Unknown"}
	allowed := map[SagaState]map[string]bool{PaymentPending: {"PaymentCharged": true, "PaymentFailed": true}, StockPending: {"StockReserved": true, "StockReservationFailed": true}, Compensating: {"PaymentRefunded": true, "PaymentRefundFailed": true}}
	for _, state := range states {
		for _, event := range events {
			if allowed[state][event] {
				continue
			}
			s := Saga{State: state, Order: Order{Status: "PENDING"}}
			before := s
			if _, err := s.Apply(event, ""); !errors.Is(err, ErrTransition) || s != before {
				t.Fatalf("accepted %s + %s: %+v %v", state, event, s, err)
			}
		}
	}
	s := Saga{State: PaymentPending}
	if _, err := s.Start(); !errors.Is(err, ErrTransition) {
		t.Fatal("restarted active saga")
	}
	if _, err := s.Apply("PaymentCharged", ""); err != nil {
		t.Fatal(err)
	}
	before := s
	if _, err := s.Apply("PaymentCharged", ""); !errors.Is(err, ErrTransition) || s != before {
		t.Fatal("duplicate must be deduplicated before applying a transition")
	}
}
