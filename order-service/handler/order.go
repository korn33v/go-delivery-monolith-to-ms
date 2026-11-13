package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/httpapi"
	"example.com/strangler/order-service/internal/domain"
	"example.com/strangler/order-service/internal/usecase"
	"github.com/gin-gonic/gin"
)

type Handler struct{ Service *usecase.Service }

func New(service *usecase.Service) *Handler { return &Handler{Service: service} }
func (h *Handler) Register(r *gin.Engine) {
	r.POST("/api/v1/orders", h.Create)
	r.GET("/api/v1/orders/:id", h.Get)
}

// Create godoc
// @Summary Оформить заказ
// @Tags orders
// @Accept json
// @Produce json
// @Param order body contract.SagaCreateOrder true "Товар и количество; demo_failure принимается только при SAGA_ENABLED и SAGA_DEMO_FAILURES"
// @Success 201 {object} contract.Order "Локальная оплата завершена"
// @Success 202 {object} contract.SagaOrder "Заказ принят; saga_id и saga_state присутствуют в режиме Saga"
// @Failure 400 {object} contract.APIError
// @Failure 404 {object} contract.APIError
// @Failure 409 {object} contract.APIError
// @Failure 501 {object} contract.APIError
// @Failure 503 {object} contract.APIError
// @Router /api/v1/orders [post]
func (h *Handler) Create(c *gin.Context) {
	var request contract.CreateOrder
	if err := httpapi.Decode(c, &request); err != nil {
		c.JSON(400, contract.APIError{Error: "invalid JSON: " + err.Error()})
		return
	}
	order, err := h.Service.Create(c.Request.Context(), domain.CreateOrder{ProductID: request.ProductID, Quantity: request.Quantity})
	if err != nil {
		respondError(c, err, order)
		return
	}
	status := http.StatusAccepted
	if order.Status == "PAID" {
		status = http.StatusCreated
	}
	c.Header("X-Backend", "order-service")
	c.JSON(status, order)
}

// Get godoc
// @Summary Получить статус заказа
// @Tags orders
// @Produce json
// @Param id path string true "ID заказа"
// @Success 200 {object} contract.SagaOrder "В режиме Strangler поля Saga отсутствуют"
// @Failure 404 {object} contract.APIError
// @Router /api/v1/orders/{id} [get]
func (h *Handler) Get(c *gin.Context) {
	order, err := h.Service.Get(c.Param("id"))
	if err != nil {
		respondError(c, err, order)
		return
	}
	c.Header("X-Backend", "order-service")
	c.JSON(200, order)
}

func respondError(c *gin.Context, err error, order domain.Order) {
	status := 500
	switch {
	case errors.Is(err, domain.ErrInvalid):
		status = 400
	case errors.Is(err, domain.ErrNotFound):
		status = 404
	case errors.Is(err, domain.ErrSoldOut):
		status = 409
	case errors.Is(err, domain.ErrPublish), errors.Is(err, domain.ErrStock):
		status = 503
	case errors.Is(err, domain.ErrNotImplemented):
		status = 501
	}
	response := contract.APIError{Error: err.Error()}
	if order.ID != "" {
		dto := contract.Order(order)
		response.Order = &dto
	}
	c.JSON(status, response)
}

func (h *Handler) PaymentResult(ctx context.Context, body []byte) error {
	var event contract.PaymentCompleted
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}
	return h.Service.Paid(event.OrderID, event.Owner, event.Status)
}
