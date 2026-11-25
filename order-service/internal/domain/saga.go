package domain

import (
	"errors"
	"fmt"
)

type SagaState string

const (
	Pending            SagaState = "PENDING"
	PaymentPending     SagaState = "PAYMENT_PENDING"
	StockPending       SagaState = "STOCK_PENDING"
	Compensating       SagaState = "COMPENSATING"
	Completed          SagaState = "COMPLETED"
	Cancelled          SagaState = "CANCELLED"
	CompensationFailed SagaState = "COMPENSATION_FAILED"
)

var ErrTransition = errors.New("invalid saga transition")

type Saga struct {
	ID           string
	Order        Order
	State        SagaState
	DemoFailure  string
	Reason       string
	PaymentFirst bool
}

// Start and Apply describe this particular business process, without Kafka/SQL.
func (s *Saga) Start() (string, error) {
	if s.State != Pending {
		return "", ErrTransition
	}
	if s.PaymentFirst {
		s.State = PaymentPending
		return "ChargePayment", nil
	}
	s.State = StockPending
	return "ReserveStock", nil
}

func (s *Saga) Apply(event, reason string) (string, error) {
	if s.PaymentFirst {
		return s.applyPaymentFirst(event, reason)
	}
	switch {
	case s.State == StockPending && event == "StockReserved":
		s.State = PaymentPending
		return "ChargePayment", nil
	case s.State == StockPending && event == "StockReservationFailed":
		s.State, s.Order.Status, s.Reason = Cancelled, "CANCELLED", reason
	case s.State == PaymentPending && event == "PaymentCharged":
		s.State, s.Order.Status = Completed, "CONFIRMED"
	case s.State == PaymentPending && event == "PaymentFailed":
		s.State, s.Reason = Compensating, reason
		return "ReleaseStock", nil
	case s.State == Compensating && event == "StockReleased":
		s.State, s.Order.Status = Cancelled, "CANCELLED"
	default:
		return "", fmt.Errorf("%w: %s + %s", ErrTransition, s.State, event)
	}
	return "", nil
}

// Sagas started before the workflow changed finish with their original rules.
func (s *Saga) applyPaymentFirst(event, reason string) (string, error) {
	switch {
	case s.State == PaymentPending && event == "PaymentCharged":
		s.State = StockPending
		return "ReserveStock", nil
	case s.State == PaymentPending && event == "PaymentFailed":
		s.State, s.Order.Status, s.Reason = Cancelled, "CANCELLED", reason
	case s.State == StockPending && event == "StockReserved":
		s.State, s.Order.Status = Completed, "CONFIRMED"
	case s.State == StockPending && event == "StockReservationFailed":
		s.State, s.Reason = Compensating, reason
		return "RefundPayment", nil
	case s.State == Compensating && event == "PaymentRefunded":
		s.State, s.Order.Status = Cancelled, "CANCELLED"
	case s.State == Compensating && event == "PaymentRefundFailed":
		s.State, s.Reason = CompensationFailed, reason
		// Money is still charged. Keep order PENDING for manual intervention.
	default:
		return "", fmt.Errorf("%w: %s + %s", ErrTransition, s.State, event)
	}
	return "", nil
}
