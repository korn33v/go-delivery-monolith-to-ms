package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/strangler/internal/observability"
	"github.com/gin-gonic/gin"
)

func TestProxyReturnsSingleGatewayTraceID(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Trace-ID", "11111111111111111111111111111111")
		w.Header().Add("X-Trace-ID", "22222222222222222222222222222222")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()
	p, err := proxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	telemetry, err := observability.New(context.Background(), "gateway", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer telemetry.Shutdown()
	r := telemetry.Router("gateway")
	r.POST("/order", func(c *gin.Context) { p.ServeHTTP(c.Writer, c.Request) })
	w := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/order", nil).WithContext(ctx))
	values := w.Header().Values("X-Trace-ID")
	if w.Code != http.StatusAccepted || len(values) != 1 || len(values[0]) != 32 {
		t.Fatalf("invalid trace header: status=%d values=%q", w.Code, values)
	}
	if values[0] == "11111111111111111111111111111111" {
		t.Fatal("gateway trace replaced by upstream")
	}
}
