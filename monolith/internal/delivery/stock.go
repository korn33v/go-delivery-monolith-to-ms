package delivery

import (
	"example.com/strangler/monolith/internal/domain"
	"github.com/gin-gonic/gin"
	"strconv"
)

// Product godoc
// @Summary Получить локальный остаток монолита
// @Tags stock
// @Produce json
// @Param id path int true "ID товара"
// @Success 200 {object} contract.Product
// @Failure 404 {object} contract.APIError
// @Router /api/v1/stock/{id} [get]
func (h *Handler) Product(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "invalid product id"})
		return
	}
	p, err := h.Service.Product(id)
	if err != nil {
		respondError(c, err, domain.Order{})
		return
	}
	c.Header("X-Backend", "monolith")
	c.JSON(200, p)
}
