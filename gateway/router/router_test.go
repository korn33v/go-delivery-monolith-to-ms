package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.com/strangler/internal/config"
	"example.com/strangler/internal/httpapi"
	"github.com/gin-gonic/gin"
)

func TestLiveFlagsAndStickyOrderOwnership(t *testing.T) {
	backend := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Backend", name); w.WriteHeader(200) }))
	}
	mono := backend("monolith")
	defer mono.Close()
	stock := backend("stock-service")
	defer stock.Close()
	orders := backend("order-service")
	defer orders.Close()
	file := filepath.Join(t.TempDir(), ".env")
	contents := "ROUTE_STOCK_TO_MICROSERVICE=false\nROUTE_ORDER_TO_MICROSERVICE=false\nASYNC_PAYMENT_ACTIVE=false\nMONOLITH_STOCK_BRIDGE_ACTIVE=false\n"
	contents += "MONOLITH_URL=" + mono.URL + "\nSTOCK_URL=" + stock.URL + "\nORDER_URL=" + orders.URL + "\n"
	if err := os.WriteFile(file, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENV_FILE", file)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(httpapi.CORS("http://localhost:5173"))
	r.GET("/api/v1/stock/:id", gateway.StockRead)
	r.POST("/api/v1/orders", gateway.CreateOrder)
	r.GET("/api/v1/orders/:id", gateway.GetOrder)
	check := func(method, path, target string) {
		t.Helper()
		w := httptest.NewRecorder()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil).WithContext(ctx))
		if w.Code != 200 || w.Header().Get("X-Backend") != target {
			t.Fatalf("%s %s: status=%d backend=%q", method, path, w.Code, w.Header().Get("X-Backend"))
		}
	}
	check("GET", "/api/v1/stock/1", "monolith")
	if err := os.WriteFile(file, []byte(strings.ReplaceAll(contents, "=false", "=true")), 0600); err != nil {
		t.Fatal(err)
	}
	check("GET", "/api/v1/stock/1", "stock-service")
	check("POST", "/api/v1/orders", "order-service")
	check("GET", "/api/v1/orders/mono-123", "monolith")
	check("GET", "/api/v1/orders/order-123", "order-service")
	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/api/v1/orders", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	r.ServeHTTP(w, req)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal("browser preflight failed")
	}
}
