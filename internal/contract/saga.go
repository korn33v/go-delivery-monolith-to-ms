package contract

type SagaCreateOrder struct {
	ProductID   int    `json:"product_id"`
	Quantity    int    `json:"quantity"`
	DemoFailure string `json:"demo_failure,omitempty" enums:"payment,refund"`
}

type SagaOrder struct {
	Order
	SagaID        string `json:"saga_id"`
	SagaState     string `json:"saga_state"`
	FailureReason string `json:"failure_reason,omitempty"`
}

type SagaAPIError struct {
	Error string     `json:"error"`
	Order *SagaOrder `json:"order,omitempty"`
}
