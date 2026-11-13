// lab changes flags in place, preserving the inode of the Docker bind mount.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"example.com/strangler/internal/config"
	"example.com/strangler/internal/contract"
)

func main() {
	reset := flag.Bool("reset", false, "allow returning to stage 1 after resetting all data")
	flag.Parse()
	if err := run(*reset, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(reset bool, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: go run ./cmd/lab [-reset] 1|2|3|4")
	}
	stage, err := strconv.Atoi(args[0])
	if err != nil || stage < 1 || stage > 4 {
		return fmt.Errorf("stage must be 1..4")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.File == "" {
		return fmt.Errorf("lab commands require a writable .env file; copy env.example to .env first")
	}
	current, err := cfg.Flags()
	if err != nil {
		return err
	}
	previous := 1
	if current.Stock {
		previous = 2
	}
	if current.Async {
		previous = 3
	}
	if current.Order {
		previous = 4
	}
	if stage < previous && !reset {
		return fmt.Errorf("backward routing also needs data reconciliation; reset ALL processes and Kafka, then use -reset 1")
	}
	if stage > previous+1 && !reset {
		return fmt.Errorf("complete stage %d first", previous+1)
	}
	client := &http.Client{Timeout: 4 * time.Second}
	if stage == 2 && previous < 2 {
		var mono, stock contract.Product
		if err := get(client, cfg.String("MONOLITH_URL", "http://localhost:8081")+"/api/v1/stock/1", &mono); err != nil {
			return err
		}
		if err := get(client, cfg.String("STOCK_URL", "http://localhost:8082")+"/api/v1/stock/1", &stock); err != nil {
			return err
		}
		if mono != stock {
			return fmt.Errorf("snapshot differs from monolith; pause purchases and restart Stock before switching")
		}
	}
	if stage == 3 {
		if err := get(client, "http://localhost:8084/health", new(any)); err != nil {
			return fmt.Errorf("start Payment Service first: %w", err)
		}
	}
	if stage == 4 {
		if err := get(client, cfg.String("ORDER_URL", "http://localhost:8083")+"/health", new(any)); err != nil {
			return fmt.Errorf("start Order Service first: %w", err)
		}
	}
	values := map[string]bool{
		"ROUTE_STOCK_TO_MICROSERVICE":  stage >= 2,
		"ROUTE_ORDER_TO_MICROSERVICE":  stage >= 4,
		"ASYNC_PAYMENT_ACTIVE":         stage >= 3,
		"MONOLITH_STOCK_BRIDGE_ACTIVE": stage >= 2,
	}
	body, err := os.ReadFile(cfg.File)
	if err != nil {
		return err
	}
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if value, exists := values[key]; exists {
			lines[i] = key + "=" + strconv.FormatBool(value)
		}
	}
	// Do not rename a temporary file: the container mounts this file's inode.
	if err := os.WriteFile(cfg.File, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return err
	}
	fmt.Printf("Stage %d enabled. No process restart needed.\n", stage)
	return nil
}
func get(client *http.Client, url string, output any) error {
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("%s returned %d", url, response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(output)
}
