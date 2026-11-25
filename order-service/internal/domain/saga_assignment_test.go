//go:build saga_assignment

package domain

import "testing"

func TestAssignmentReserveBeforePayment(t *testing.T) {
	s := Saga{State: Pending, Order: Order{Status: "PENDING"}}
	next, err := s.Start()
	if err != nil || next != "ReserveStock" || s.State != StockPending || s.Order.Status != "PENDING" {
		t.Fatalf("AC-01: want ReserveStock with Saga STOCK_PENDING and order PENDING, got command=%q saga=%s order=%s err=%v", next, s.State, s.Order.Status, err)
	}
	next, err = s.Apply("StockReserved", "")
	if err != nil || next != "ChargePayment" || s.State != PaymentPending || s.Order.Status != "PENDING" {
		t.Fatalf("AC-01: after StockReserved want ChargePayment and order PENDING, got command=%q saga=%s order=%s err=%v", next, s.State, s.Order.Status, err)
	}
	next, err = s.Apply("PaymentCharged", "")
	if err != nil || next != "" || s.State != Completed || s.Order.Status != "CONFIRMED" {
		t.Fatalf("AC-01: after PaymentCharged want CONFIRMED without another command, got command=%q saga=%s order=%s err=%v", next, s.State, s.Order.Status, err)
	}
}

func TestAssignmentNoPaymentWhenStockRejects(t *testing.T) {
	s := Saga{State: StockPending, Order: Order{Status: "PENDING"}}
	next, err := s.Apply("StockReservationFailed", "insufficient_stock")
	if err != nil || next != "" || s.State != Cancelled || s.Order.Status != "CANCELLED" || s.Reason != "insufficient_stock" {
		t.Fatalf("AC-02: want CANCELLED without payment or refund, got command=%q saga=%s order=%s reason=%q err=%v", next, s.State, s.Order.Status, s.Reason, err)
	}
}

func TestAssignmentDeclinedPaymentReleasesStock(t *testing.T) {
	s := Saga{State: PaymentPending, Order: Order{Status: "PENDING"}}
	next, err := s.Apply("PaymentFailed", "demo_payment_declined")
	if err != nil || next != "ReleaseStock" || s.State != Compensating || s.Order.Status != "PENDING" || s.Reason != "demo_payment_declined" {
		t.Fatalf("AC-03/04: want ReleaseStock and order PENDING until confirmation, got command=%q saga=%s order=%s reason=%q err=%v", next, s.State, s.Order.Status, s.Reason, err)
	}
}

func TestAssignmentCancelAfterStockReleased(t *testing.T) {
	s := Saga{State: Compensating, Order: Order{Status: "PENDING"}, Reason: "demo_payment_declined"}
	next, err := s.Apply("StockReleased", "")
	if err != nil || next != "" || s.State != Cancelled || s.Order.Status != "CANCELLED" || s.Reason != "demo_payment_declined" {
		t.Fatalf("AC-04: want CANCELLED after StockReleased with the original reason, got command=%q saga=%s order=%s reason=%q err=%v", next, s.State, s.Order.Status, s.Reason, err)
	}
}
