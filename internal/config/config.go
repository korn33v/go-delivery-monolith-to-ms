package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	File    string
	initial map[string]string
}

// Load prefers a physical .env for live flag reload, but also supports an
// environment-only container: Compose env_file injects variables, not a file.
func Load() (*Config, error) {
	file := os.Getenv("ENV_FILE")
	if file == "" {
		for _, candidate := range []string{".env", "../.env"} {
			if _, err := os.Stat(candidate); err == nil {
				file = candidate
				break
			}
		}
	}
	c := &Config{initial: make(map[string]string)}
	if file != "" {
		absolute, err := filepath.Abs(file)
		if err != nil {
			return nil, err
		}
		values, err := godotenv.Read(absolute)
		if err == nil {
			c.File = absolute
			c.initial = values
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read config %q: %w; ensure it is a readable file (for this lab: chmod 644 .env)", absolute, err)
		}
	}
	if _, err := c.Flags(); err != nil {
		if c.File == "" {
			return nil, fmt.Errorf("no .env file available: copy env.example to .env in the selected start/solution directory, set ENV_FILE to a readable file, or supply all four migration flags in the process environment: %w", err)
		}
		return nil, err
	}
	return c, nil
}

func (c *Config) String(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	if value, ok := c.initial[key]; ok {
		return value
	}
	return fallback
}

func (c *Config) Int(key string, fallback int) int {
	value := c.String(key, strconv.Itoa(fallback))
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		panic("invalid non-negative integer: " + key)
	}
	return n
}

func (c *Config) Bool(key string, fallback bool) bool {
	value, err := strconv.ParseBool(c.String(key, strconv.FormatBool(fallback)))
	if err != nil {
		panic("invalid boolean: " + key)
	}
	return value
}

func (c *Config) Brokers() []string {
	return strings.Split(c.String("KAFKA_BROKERS", "localhost:19092"), ",")
}

type Flags struct {
	Stock  bool `json:"stock_microservice"`
	Order  bool `json:"order_microservice"`
	Async  bool `json:"async_payment"`
	Bridge bool `json:"stock_bridge"`
}

// Read the file into a request-local snapshot: no process-wide os.Setenv races.
// A mounted file is authoritative. Without a file, use injected process values.
func (c *Config) Flags() (Flags, error) {
	values := make(map[string]string)
	source := "process environment"
	if c.File != "" {
		var err error
		values, err = godotenv.Read(c.File)
		if err != nil {
			return Flags{}, fmt.Errorf("reload config %q: %w", c.File, err)
		}
		source = c.File
	}
	f := Flags{}
	fields := map[string]*bool{
		"ROUTE_STOCK_TO_MICROSERVICE":  &f.Stock,
		"ROUTE_ORDER_TO_MICROSERVICE":  &f.Order,
		"ASYNC_PAYMENT_ACTIVE":         &f.Async,
		"MONOLITH_STOCK_BRIDGE_ACTIVE": &f.Bridge,
	}
	for key, field := range fields {
		if c.File == "" {
			values[key] = os.Getenv(key)
		}
		parsed, err := strconv.ParseBool(values[key])
		if err != nil {
			return Flags{}, fmt.Errorf("invalid migration flag %s from %s: %w", key, source, err)
		}
		*field = parsed
	}
	return f, nil
}
