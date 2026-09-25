// Package config reads configuration from the environment.
//
// Everything that differs between a laptop and Compose — addresses, the model
// name, timeouts — is an environment variable with a working default, so the
// binaries run with no setup and are still configurable in a container.
package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

// String returns the environment variable, or fallback if it is unset or empty.
func String(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Int returns the environment variable parsed as an int. An unparseable value
// is logged and ignored rather than crashing the service.
func Int(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("invalid integer in environment, using default", "key", key, "value", raw, "default", fallback)
		return fallback
	}
	return v
}

// Duration returns the environment variable parsed as a Go duration ("90s").
func Duration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		slog.Warn("invalid duration in environment, using default", "key", key, "value", raw, "default", fallback)
		return fallback
	}
	return v
}

// Logger builds the structured logger both binaries use. JSON on stdout is
// what a container log collector expects.
func Logger(service string) *slog.Logger {
	level := slog.LevelInfo
	if String("LOG_LEVEL", "info") == "debug" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).
		With("service", service)
}
