package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"example.com/strangler/internal/config"
	"example.com/strangler/internal/contract"
)

var client = &http.Client{Timeout: 12 * time.Second}

func main() {
	stage := flag.Int("stage", 1, "expected migration stage 1..4")
	traces := flag.Bool("require-trace", false, "also assert full trace in Jaeger")
	flag.Parse()
	if err := run(*stage, *traces); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
	fmt.Printf("SUCCESS: stage %d, order paid, stock decremented once\n", *stage)
}
func run(stage int, traces bool) error {
	if stage < 1 || stage > 4 {
		return fmt.Errorf("stage must be 1..4")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	gateway := "http://localhost:8080"
	var f config.Flags
	if _, _, err = request("GET", gateway+"/api/v1/migration", nil, &f); err != nil {
		return err
	}
	if f.Stock != (stage >= 2) || f.Order != (stage == 4) || f.Async != (stage >= 3) || f.Bridge != (stage >= 2) {
		return fmt.Errorf("flags do not match stage %d; run make stage%d", stage, stage)
	}
	ports := []int{8080, 8081}
	if stage >= 2 {
		ports = append(ports, 8082)
	}
	if stage == 4 {
		ports = append(ports, 8083)
	}
	for _, port := range ports {
		var spec map[string]any
		if _, _, err = request("GET", fmt.Sprintf("http://localhost:%d/swagger/doc.json", port), nil, &spec); err != nil {
			return err
		}
		if spec["swagger"] != "2.0" {
			return fmt.Errorf("invalid Swagger on %d", port)
		}
	}
	var before, after contract.Product
	_, headers, err := request("GET", gateway+"/api/v1/stock/1", nil, &before)
	if err != nil {
		return err
	}
	stockBackend := "monolith"
	if stage >= 2 {
		stockBackend = "stock-service"
	}
	if headers.Get("X-Backend") != stockBackend {
		return fmt.Errorf("stock route: got %q", headers.Get("X-Backend"))
	}
	var order contract.Order
	status, headers, err := request("POST", gateway+"/api/v1/orders", contract.CreateOrder{ProductID: 1, Quantity: 1}, &order)
	if err != nil {
		return fmt.Errorf("create order: %w; in start/ complete the TODO blocks before stage 4", err)
	}
	expected := 201
	if stage >= 3 {
		expected = 202
	}
	if status != expected {
		return fmt.Errorf("create status %d, expected %d", status, expected)
	}
	expectedOwner := "monolith"
	if stage == 4 {
		expectedOwner = "order-service"
	}
	if order.Owner != expectedOwner {
		return fmt.Errorf("order owner %q", order.Owner)
	}
	traceID := headers.Get("X-Trace-ID")
	if len(traceID) != 32 {
		return fmt.Errorf("missing trace ID")
	}
	id := order.ID
	deadline := time.Now().Add(35 * time.Second)
	for order.Status != "PAID" && time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		if _, _, err = request("GET", gateway+"/api/v1/orders/"+id, nil, &order); err != nil {
			return err
		}
	}
	if order.Status != "PAID" {
		return fmt.Errorf("order %s is %s; inspect Payment and result-consumer logs", id, order.Status)
	}
	if _, _, err = request("GET", gateway+"/api/v1/stock/1", nil, &after); err != nil {
		return err
	}
	if after.Available != before.Available-1 {
		return fmt.Errorf("stock: before %d, after %d", before.Available, after.Available)
	}
	if stage == 2 || stage == 3 {
		var legacy contract.Product
		if _, _, err = request("GET", cfg.String("MONOLITH_URL", "http://localhost:8081")+"/api/v1/stock/1", nil, &legacy); err != nil {
			return err
		}
		if legacy.Available != after.Available {
			return fmt.Errorf("legacy and extracted stock diverged")
		}
	}
	if stage == 4 {
		var result contract.APIError
		code, _, _ := request("POST", gateway+"/api/v1/orders", contract.CreateOrder{ProductID: 1, Quantity: 0}, &result)
		if code != 400 {
			return fmt.Errorf("zero quantity must return 400, got %d", code)
		}
	}
	metrics, err := client.Get(gateway + "/metrics")
	if err != nil {
		return err
	}
	body, err := io.ReadAll(metrics.Body)
	metrics.Body.Close()
	if err != nil {
		return err
	}
	if !strings.Contains(string(body), "http_requests_total") {
		return fmt.Errorf("missing HTTP metrics")
	}
	fmt.Printf("order=%s trace_id=%s stock=%d owner=%s\n", id, traceID, after.Available, order.Owner)
	if traces {
		return verifyTrace(traceID, stage)
	}
	return nil
}

func request(method, url string, input, output any) (int, http.Header, error) {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, nil, err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		data, _ := io.ReadAll(response.Body)
		return response.StatusCode, response.Header, fmt.Errorf("HTTP %d: %s", response.StatusCode, data)
	}
	if output != nil {
		err = json.NewDecoder(response.Body).Decode(output)
	}
	return response.StatusCode, response.Header, err
}

func verifyTrace(id string, stage int) error {
	expected := []string{"gateway", "monolith"}
	if stage >= 2 {
		expected = append(expected, "stock-service")
	}
	if stage >= 3 {
		expected = append(expected, "payment-service")
	}
	if stage == 4 {
		expected = []string{"gateway", "stock-service", "order-service", "payment-service"}
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var result struct {
			Data []struct {
				Processes map[string]struct {
					ServiceName string `json:"serviceName"`
				} `json:"processes"`
			} `json:"data"`
		}
		_, _, err := request("GET", "http://localhost:16686/api/traces/"+id, nil, &result)
		if err == nil && len(result.Data) > 0 {
			found := map[string]bool{}
			for _, process := range result.Data[0].Processes {
				found[process.ServiceName] = true
			}
			complete := true
			for _, service := range expected {
				if !found[service] {
					complete = false
				}
			}
			if complete {
				fmt.Println("Jaeger: one trace contains", strings.Join(expected, ", "))
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("trace %s does not contain all expected services: %v", id, expected)
}
