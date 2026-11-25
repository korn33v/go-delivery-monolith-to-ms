package domain

import "errors"

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
	ID          string
	Order       Order
	State       SagaState
	DemoFailure string
	Reason      string
}

// Start begins the order workflow and returns the first command.
func (s *Saga) Start() (string, error) {
	// TODO для студентов: проверьте начальное состояние и выберите команду.
	return "", errors.New("TODO Saga.Start")
}

// Apply consumes one participant event and returns the next command, if any.
func (s *Saga) Apply(event, reason string) (string, error) {
	// TODO для студентов: учтите текущий шаг, событие и причину отказа.
	// Заказ ожидает подтверждения возврата до завершения компенсации.
	return "", errors.New("TODO Saga.Apply")
}
