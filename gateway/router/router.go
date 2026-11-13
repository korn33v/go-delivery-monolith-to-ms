package router

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"example.com/strangler/internal/config"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Gateway struct {
	Config   *config.Config
	Monolith *httputil.ReverseProxy
	Stock    *httputil.ReverseProxy
	Order    *httputil.ReverseProxy
}

func proxy(address string) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	if target.Scheme != "http" || target.Host == "" {
		return nil, &url.Error{Op: "parse", URL: address, Err: http.ErrNotSupported}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 8 * time.Second
	p := &httputil.ReverseProxy{
		Rewrite:   func(r *httputil.ProxyRequest) { r.SetURL(target); r.SetXForwarded() },
		Transport: otelhttp.NewTransport(transport),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("upstream unavailable", "target", address, "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"upstream unavailable; start the selected service"}`))
		},
	}
	return p, nil
}

func New(c *config.Config) (*Gateway, error) {
	mono, err := proxy(c.String("MONOLITH_URL", "http://localhost:8081"))
	if err != nil {
		return nil, err
	}
	stock, err := proxy(c.String("STOCK_URL", "http://localhost:8082"))
	if err != nil {
		return nil, err
	}
	order, err := proxy(c.String("ORDER_URL", "http://localhost:8083"))
	if err != nil {
		return nil, err
	}
	return &Gateway{Config: c, Monolith: mono, Stock: stock, Order: order}, nil
}

// StockRead godoc
// @Summary Остаток через переключаемый маршрут
// @Tags stock
// @Produce json
// @Param id path int true "ID товара"
// @Success 200 {object} contract.Product
// @Failure 404 {object} contract.APIError
// @Failure 502 {object} contract.APIError
// @Router /api/v1/stock/{id} [get]
func (g *Gateway) StockRead(c *gin.Context) {
	flags, err := g.Config.Flags()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if flags.Stock {
		g.Stock.ServeHTTP(c.Writer, c.Request)
	} else {
		g.Monolith.ServeHTTP(c.Writer, c.Request)
	}
}

// CreateOrder godoc
// @Summary Создать заказ через переключаемый маршрут
// @Tags orders
// @Accept json
// @Produce json
// @Param order body contract.SagaCreateOrder true "Товар и количество; demo_failure только в учебном режиме Saga"
// @Success 201 {object} contract.Order
// @Success 202 {object} contract.SagaOrder
// @Failure 400 {object} contract.APIError
// @Failure 409 {object} contract.APIError
// @Failure 501 {object} contract.APIError
// @Failure 502 {object} contract.APIError
// @Failure 503 {object} contract.APIError
// @Router /api/v1/orders [post]
func (g *Gateway) CreateOrder(c *gin.Context) { g.orders(c) }

// GetOrder godoc
// @Summary Статус заказа; старые ID продолжают обслуживаться монолитом
// @Tags orders
// @Produce json
// @Param id path string true "ID заказа"
// @Success 200 {object} contract.SagaOrder "Поля Saga присутствуют при SAGA_ENABLED"
// @Failure 404 {object} contract.APIError
// @Failure 502 {object} contract.APIError
// @Router /api/v1/orders/{id} [get]
func (g *Gateway) GetOrder(c *gin.Context) { g.orders(c) }

func (g *Gateway) orders(c *gin.Context) {
	flags, err := g.Config.Flags()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	useNew := flags.Order
	// Sticky ownership for GET prevents migrated routing from hiding old data.
	if c.Request.Method == http.MethodGet {
		id := c.Param("id")
		if strings.HasPrefix(id, "mono-") {
			useNew = false
		}
		if strings.HasPrefix(id, "order-") {
			useNew = true
		}
	}
	if useNew {
		g.Order.ServeHTTP(c.Writer, c.Request)
	} else {
		g.Monolith.ServeHTTP(c.Writer, c.Request)
	}
}

// Migration godoc
// @Summary Текущие флаги миграции (только чтение)
// @Tags migration
// @Produce json
// @Success 200 {object} config.Flags
// @Router /api/v1/migration [get]
func (g *Gateway) Migration(c *gin.Context) {
	f, err := g.Config.Flags()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, f)
}
