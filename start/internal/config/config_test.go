package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var flagKeys = []string{"ROUTE_STOCK_TO_MICROSERVICE", "ROUTE_ORDER_TO_MICROSERVICE", "ASYNC_PAYMENT_ACTIVE", "MONOLITH_STOCK_BRIDGE_ACTIVE"}

func environment(t *testing.T) {
	t.Helper()
	for _, key := range flagKeys {
		t.Setenv(key, "false")
	}
}

func workingDirectory(t *testing.T) string {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func TestEnvironmentOnlyWithoutDotEnv(t *testing.T) {
	workingDirectory(t)
	t.Setenv("ENV_FILE", "")
	environment(t)
	t.Setenv("KAFKA_BROKERS", "kafka:9092")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Compose env_file variables must be sufficient without a physical file: %v", err)
	}
	if cfg.File != "" {
		t.Fatalf("unexpected file: %s", cfg.File)
	}
	if cfg.String("KAFKA_BROKERS", "") != "kafka:9092" {
		t.Fatal("environment was ignored")
	}
	f, err := cfg.Flags()
	if err != nil || f != (Flags{}) {
		t.Fatalf("flags=%+v error=%v", f, err)
	}
}

func TestExplicitMissingFileFallsBackToEnvironment(t *testing.T) {
	dir := workingDirectory(t)
	t.Setenv("ENV_FILE", filepath.Join(dir, "missing", ".env"))
	environment(t)
	t.Setenv("ASYNC_PAYMENT_ACTIVE", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	f, err := cfg.Flags()
	if err != nil || !f.Async {
		t.Fatalf("flags=%+v error=%v", f, err)
	}
}

func TestNoFileAndIncompleteEnvironmentExplainsRecovery(t *testing.T) {
	workingDirectory(t)
	t.Setenv("ENV_FILE", "")
	for _, key := range flagKeys {
		t.Setenv(key, "")
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "env.example") {
		t.Fatalf("expected actionable missing configuration error, got %v", err)
	}
}

func TestFileFlagsRemainAuthoritativeAndReloadLive(t *testing.T) {
	dir := workingDirectory(t)
	file := filepath.Join(dir, ".env")
	t.Setenv("ENV_FILE", file)
	environment(t)
	t.Setenv("ROUTE_STOCK_TO_MICROSERVICE", "true")
	text := ""
	for _, key := range flagKeys {
		text += key + "=false\n"
	}
	if err := os.WriteFile(file, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	f, err := cfg.Flags()
	if err != nil || f.Stock {
		t.Fatalf("file must win over injected flags: %+v %v", f, err)
	}
	text = strings.Replace(text, "ROUTE_STOCK_TO_MICROSERVICE=false", "ROUTE_STOCK_TO_MICROSERVICE=true", 1)
	if err := os.WriteFile(file, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	f, err = cfg.Flags()
	if err != nil || !f.Stock {
		t.Fatalf("live file reload failed: %+v %v", f, err)
	}
}

func TestMalformedFileIsNotSilentlyIgnored(t *testing.T) {
	dir := workingDirectory(t)
	file := filepath.Join(dir, ".env")
	t.Setenv("ENV_FILE", file)
	environment(t)
	if err := os.WriteFile(file, []byte("BROKEN='unterminated\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("malformed file must fail even when environment variables exist")
	}
}
