// Package observability provides logging, metrics, and tracing infrastructure.
package observability

import (
	"context"
	"log/slog"
	"os"
)

// LogLevel represents logging levels.
type LogLevel string

const (
	// LogLevelDebug enables debug logging.
	LogLevelDebug LogLevel = "debug"

	// LogLevelInfo enables info logging (default).
	LogLevelInfo LogLevel = "info"

	// LogLevelWarn enables warning logging.
	LogLevelWarn LogLevel = "warn"

	// LogLevelError enables error logging only.
	LogLevelError LogLevel = "error"
)

// Config holds observability configuration.
type Config struct {
	// LogLevel sets the minimum log level.
	LogLevel LogLevel

	// ServiceName identifies this service in traces and logs.
	ServiceName string

	// ServiceVersion is the version of this service.
	ServiceVersion string

	// Environment (e.g., "development", "production").
	Environment string

	// EnableTracing enables OpenTelemetry tracing.
	EnableTracing bool

	// TraceExporterEndpoint is the OTLP endpoint for traces.
	TraceExporterEndpoint string
}

// Setup initializes the observability stack (logging and tracing).
func Setup(ctx context.Context, cfg Config) (*slog.Logger, func(), error) {
	// Convert log level
	var level slog.Level
	switch cfg.LogLevel {
	case LogLevelDebug:
		level = slog.LevelDebug
	case LogLevelInfo:
		level = slog.LevelInfo
	case LogLevelWarn:
		level = slog.LevelWarn
	case LogLevelError:
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Create base handler (JSON for production, text for development)
	var baseHandler slog.Handler
	if cfg.Environment == "production" {
		baseHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     level,
			AddSource: true,
		})
	} else {
		baseHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level:     level,
			AddSource: true,
		})
	}

	// For Phase 1, we're using basic slog
	// Full OpenTelemetry integration will be added in Phase 4
	logger := slog.New(baseHandler)
	cleanup := func() {}

	// Add service metadata to all logs
	logger = logger.With(
		"service", cfg.ServiceName,
		"version", cfg.ServiceVersion,
		"environment", cfg.Environment,
	)

	return logger, cleanup, nil
}

// WithFields adds structured fields to a context for logging.
func WithFields(ctx context.Context, fields map[string]any) context.Context {
	// This is a placeholder for adding fields to context
	// In a real implementation, you'd store these in context and extract in logging
	return ctx
}
