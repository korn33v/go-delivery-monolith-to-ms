package handler

import (
	"context"
	"encoding/json"
	"errors"

	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/httpapi"
	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/order-service/internal/domain"
	"example.com/strangler/order-service/internal/usecase"
	"github.com/gin-gonic/gin"
)

type SagaHandler struct{ Service *usecase.Orchestrator }

func sagaDTO(s domain.Saga) contract.SagaOrder {
	return contract.SagaOrder{Order: contract.Order{ID: s.Order.ID, ProductID: s.Order.ProductID, Quantity: s.Order.Quantity, AmountCents: s.Order.AmountCents, Status: s.Order.Status, Owner: s.Order.Owner}, SagaID: s.ID, SagaState: string(s.State), FailureReason: s.Reason}
}

// CreateSaga godoc
// @Summary Создать заказ через Saga (SAGA_ENABLED=true)
// @Tags saga
// @Accept json
// @Produce json
// @Param order body contract.SagaCreateOrder true "Товар 1: в наличии; товар 2: нет остатка; demo_failure только для семинара"
// @Success 202 {object} contract.SagaOrder
// @Failure 400 {object} contract.SagaAPIError
// @Failure 503 {object} contract.SagaAPIError
func (h *SagaHandler) Create(c *gin.Context) {
	var request contract.SagaCreateOrder
	if err := httpapi.Decode(c, &request); err != nil {
		c.JSON(400, contract.SagaAPIError{Error: err.Error()})
		return
	}
	saga, err := h.Service.Create(c.Request.Context(), domain.CreateOrder{ProductID: request.ProductID, Quantity: request.Quantity}, request.DemoFailure)
	if err != nil {
		status := 503
		if errors.Is(err, domain.ErrInvalid) {
			status = 400
		}
		response := contract.SagaAPIError{Error: err.Error()}
		if errors.Is(err, domain.ErrPublish) {
			dto := sagaDTO(saga)
			response.Order = &dto
		}
		c.JSON(status, response)
		return
	}
	c.Header("X-Backend", "order-service")
	c.JSON(202, sagaDTO(saga))
}

// GetSaga godoc
// @Summary Прочитать заказ и текущий шаг Saga (SAGA_ENABLED=true)
// @Tags saga
// @Produce json
// @Param id path string true "ID заказа"
// @Success 200 {object} contract.SagaOrder
// @Failure 404 {object} contract.SagaAPIError
// @Failure 503 {object} contract.SagaAPIError
func (h *SagaHandler) Get(c *gin.Context) {
	saga, err := h.Service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		status := 503
		if errors.Is(err, domain.ErrNotFound) {
			status = 404
		}
		c.JSON(status, contract.SagaAPIError{Error: err.Error()})
		return
	}
	c.Header("X-Backend", "order-service")
	c.JSON(200, sagaDTO(saga))
}

func (h *SagaHandler) Event(ctx context.Context, body []byte) error {
	var event sagacontract.Message
	if err := json.Unmarshal(body, &event); err != nil {
		return messageerror.Permanent(err)
	}
	return h.Service.Handle(ctx, event)
}
