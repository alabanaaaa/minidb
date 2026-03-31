package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("HTTP_PORT")
	os.Unsetenv("JWT_SECRET")
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("APP_ENV")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DatabaseURL != "postgres://postgres:postgres@localhost:5432/minidb?sslmode=disable" {
		t.Errorf("wrong default DB URL: %s", cfg.DatabaseURL)
	}
	if cfg.HTTPPort != 8080 {
		t.Errorf("wrong default port: %d", cfg.HTTPPort)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("wrong default log level: %s", cfg.LogLevel)
	}
	if cfg.Env != "development" {
		t.Errorf("wrong default env: %s", cfg.Env)
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("HTTP_PORT", "9999")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("APP_ENV", "staging")
	os.Setenv("JWT_SECRET", "test-secret")
	defer func() {
		os.Unsetenv("HTTP_PORT")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("APP_ENV")
		os.Unsetenv("JWT_SECRET")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HTTPPort != 9999 {
		t.Errorf("expected port 9999, got %d", cfg.HTTPPort)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected debug, got %s", cfg.LogLevel)
	}
	if cfg.Env != "staging" {
		t.Errorf("expected staging, got %s", cfg.Env)
	}
}

func TestProductionRequiresJWTSecret(t *testing.T) {
	os.Setenv("APP_ENV", "production")
	os.Unsetenv("JWT_SECRET")
	defer func() {
		os.Unsetenv("APP_ENV")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when JWT_SECRET is missing in production")
	}
}

func TestGetEnvIntInvalid(t *testing.T) {
	os.Setenv("HTTP_PORT", "not-a-number")
	defer os.Unsetenv("HTTP_PORT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HTTPPort != 8080 {
		t.Errorf("expected fallback port 8080, got %d", cfg.HTTPPort)
	}
}
