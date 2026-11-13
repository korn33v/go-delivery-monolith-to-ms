package delivery

import (
	"context"
	"encoding/json"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/payment-service/internal/usecase"
)

type SagaHandler struct{ Service *usecase.SagaService }

func (h *SagaHandler) Command(ctx context.Context, body []byte) error {
	var command sagacontract.Message
	if err := json.Unmarshal(body, &command); err != nil {
		return err
	}
	return h.Service.Handle(ctx, command)
}
