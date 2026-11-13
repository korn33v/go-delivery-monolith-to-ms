package domain

import "errors"

var ErrPaymentTransition = errors.New("payment operation has no valid prior charge")

type SagaPayment struct {
	OrderID     string
	SagaID      string
	AmountCents int
	ProductID   int
	Quantity    int
	DemoFailure string
	State       string
	Charges     int
	Refunds     int
}

func (p *SagaPayment) Charge() (string, string, error) {
	if p.State != "NEW" {
		return "", "", ErrPaymentTransition
	}
	if p.DemoFailure == "payment" {
		p.State = "DECLINED"
		return "PaymentFailed", "demo_payment_declined", nil
	}
	p.State = "CHARGED"
	p.Charges++
	return "PaymentCharged", "", nil
}
func (p *SagaPayment) Refund() (string, string, error) {
	if p.State != "CHARGED" {
		return "", "", ErrPaymentTransition
	}
	if p.DemoFailure == "refund" {
		p.State = "REFUND_FAILED"
		return "PaymentRefundFailed", "demo_refund_declined", nil
	}
	p.State = "REFUNDED"
	p.Refunds++
	return "PaymentRefunded", "", nil
}
