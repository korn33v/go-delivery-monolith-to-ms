package delivery

import (
	"errors"
	"strconv"

	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/httpapi"
	"example.com/strangler/stock-service/internal/domain"
	"example.com/strangler/stock-service/internal/usecase"
	"github.com/gin-gonic/gin"
)

type Handler struct{ Service *usecase.Service }

func New(service *usecase.Service) *Handler { return &Handler{Service: service} }

// Get godoc
// @Summary Получить остаток извлечённого склада
// @Tags stock
// @Produce json
// @Param id path int true "ID товара"
// @Success 200 {object} contract.Product
// @Failure 400 {object} contract.APIError
// @Failure 404 {object} contract.APIError
// @Router /api/v1/stock/{id} [get]
func (h *Handler) Get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		c.JSON(400, gin.H{"error": "invalid product id"})
		return
	}
	product, err := h.Service.Get(id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("X-Backend", "stock-service")
	c.JSON(200, product)
}

// Reserve godoc
// @Summary Атомарно проверить и списать остаток
// @Description Повтор с тем же order_id и телом не списывает товар второй раз.
// @Tags stock
// @Accept json
// @Produce json
// @Param reservation body contract.Reservation true "Резервирование"
// @Success 200 {object} contract.Product
// @Failure 400 {object} contract.APIError
// @Failure 404 {object} contract.APIError
// @Failure 409 {object} contract.APIError
// @Router /api/v1/stock/reservations [post]
func (h *Handler) Reserve(c *gin.Context) {
	var request contract.Reservation
	if err := httpapi.Decode(c, &request); err != nil {
		c.JSON(400, gin.H{"error": "invalid JSON"})
		return
	}
	product, err := h.Service.Reserve(domain.Reservation{OrderID: request.OrderID, ProductID: request.ProductID, Quantity: request.Quantity})
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("X-Backend", "stock-service")
	c.JSON(200, product)
}
func respondError(c *gin.Context, err error) {
	status := 500
	switch {
	case errors.Is(err, domain.ErrInvalid):
		status = 400
	case errors.Is(err, domain.ErrNotFound):
		status = 404
	case errors.Is(err, domain.ErrConflict):
		status = 409
	}
	c.JSON(status, contract.APIError{Error: err.Error()})
}
