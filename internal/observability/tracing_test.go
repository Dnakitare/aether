package observability_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"go.opentelemetry.io/otel/attribute"

	"github.com/aether-runtime/aether/internal/observability"
)

func TestTracerProvider(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := observability.TracerConfig{
		Enabled:       true,
		UseStdout:     true, // Use stdout for testing
		SamplingRatio: 1.0,  // Sample all traces
		Environment:   "test",
	}

	tp, err := observability.NewTracerProvider(logger, config)
	if err != nil {
		t.Fatalf("Failed to create tracer provider: %v", err)
	}
	defer tp.Shutdown(context.Background())

	// Test tracer creation
	tracer := tp.Tracer("test-tracer")
	if tracer == nil {
		t.Fatal("Expected tracer to be created")
	}

	// Test span creation
	ctx := context.Background()
	ctx, span := observability.StartSpan(ctx, tracer, "test-operation",
		attribute.String("test", "value"),
	)
	defer span.End()

	// Test span attributes
	observability.AddSpanAttributes(ctx,
		attribute.String("key", "value"),
		attribute.Int("count", 42),
	)

	// Test span events
	observability.AddSpanEvent(ctx, "test-event",
		attribute.String("event", "data"),
	)

	// Test trace ID extraction
	traceID := observability.TraceIDFromContext(ctx)
	if traceID == "" {
		t.Error("Expected trace ID to be set")
	}

	spanID := observability.SpanIDFromContext(ctx)
	if spanID == "" {
		t.Error("Expected span ID to be set")
	}
}

func TestTracerProviderDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := observability.TracerConfig{
		Enabled: false,
	}

	tp, err := observability.NewTracerProvider(logger, config)
	if err != nil {
		t.Fatalf("Failed to create tracer provider: %v", err)
	}
	defer tp.Shutdown(context.Background())

	// Should still create a tracer, just no-op
	tracer := tp.Tracer("test-tracer")
	if tracer == nil {
		t.Fatal("Expected tracer to be created even when disabled")
	}
}
