package domain

import (
	"errors"
	"testing"
)

func TestPaymentBusinessResults(t *testing.T) {
	for _, failure := range []string{"", "payment", "refund"} {
		t.Run(failure, func(t *testing.T) {
			p := SagaPayment{State: "NEW", DemoFailure: failure}
			event, _, err := p.Charge()
			if err != nil {
				t.Fatal(err)
			}
			if failure == "payment" {
				if event != "PaymentFailed" || p.Charges != 0 {
					t.Fatal(p)
				}
				return
			}
			if event != "PaymentCharged" || p.Charges != 1 {
				t.Fatal(p)
			}
			event, _, err = p.Refund()
			if err != nil {
				t.Fatal(err)
			}
			if failure == "refund" {
				if event != "PaymentRefundFailed" || p.Refunds != 0 || p.State != "REFUND_FAILED" {
					t.Fatal(p)
				}
				return
			}
			if event != "PaymentRefunded" || p.Refunds != 1 {
				t.Fatal(p)
			}
			if _, _, err = p.Refund(); !errors.Is(err, ErrPaymentTransition) || p.Refunds != 1 {
				t.Fatal("double refund")
			}
		})
	}
}
func TestPaymentCannotRefundUnchargedOrChargeTwice(t *testing.T) {
	p := SagaPayment{State: "NEW"}
	if _, _, err := p.Refund(); !errors.Is(err, ErrPaymentTransition) {
		t.Fatal("refund without charge")
	}
	if _, _, err := p.Charge(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Charge(); !errors.Is(err, ErrPaymentTransition) || p.Charges != 1 {
		t.Fatal("double charge")
	}
}
