package delivery

import (
	"context"
	"encoding/json"
	"example.com/strangler/internal/contract"
	"example.com/strangler/payment-service/internal/domain"
	"example.com/strangler/payment-service/internal/usecase"
)

type Handler struct{ Service *usecase.Service }

func (h *Handler) Created(ctx context.Context, body []byte) error {
	var event contract.OrderCreated
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}
	return h.Service.Pay(ctx, domain.Order{ID: event.OrderID, Owner: event.Owner, AmountCents: event.AmountCents, Quantity: event.Quantity})
}
