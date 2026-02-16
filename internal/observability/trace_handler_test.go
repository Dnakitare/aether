package observability

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
)

func TestTraceHandler_AddsTraceContext(t *testing.T) {
	// Set up a tracer provider
	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create a logger with trace handler
	baseHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	traceHandler := NewTraceHandler(baseHandler)
	logger := slog.New(traceHandler)

	// Create a traced context
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-operation")
	defer span.End()

	// Log with trace context
	logger.InfoContext(ctx, "test message", "key", "value")

	// Verify trace_id and span_id are in the output
	output := buf.String()

	if !strings.Contains(output, "trace_id=") {
		t.Errorf("Expected log output to contain trace_id, got: %s", output)
	}

	if !strings.Contains(output, "span_id=") {
		t.Errorf("Expected log output to contain span_id, got: %s", output)
	}

	if !strings.Contains(output, "test message") {
		t.Errorf("Expected log output to contain message, got: %s", output)
	}

	if !strings.Contains(output, "key=value") {
		t.Errorf("Expected log output to contain custom field, got: %s", output)
	}
}

func TestTraceHandler_NoTraceContext(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create a logger with trace handler
	baseHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	traceHandler := NewTraceHandler(baseHandler)
	logger := slog.New(traceHandler)

	// Log without trace context (no active span)
	ctx := context.Background()
	logger.InfoContext(ctx, "test message without trace")

	// Verify trace_id and span_id are NOT in the output
	output := buf.String()

	if strings.Contains(output, "trace_id=") {
		t.Errorf("Expected log output to NOT contain trace_id when no span, got: %s", output)
	}

	if strings.Contains(output, "span_id=") {
		t.Errorf("Expected log output to NOT contain span_id when no span, got: %s", output)
	}

	if !strings.Contains(output, "test message without trace") {
		t.Errorf("Expected log output to contain message, got: %s", output)
	}
}

func TestTraceHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer

	baseHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	traceHandler := NewTraceHandler(baseHandler)

	// Add attributes to the handler
	loggerWithAttrs := slog.New(traceHandler.WithAttrs([]slog.Attr{
		slog.String("service", "test-service"),
		slog.String("version", "1.0.0"),
	}))

	loggerWithAttrs.Info("test message")

	output := buf.String()

	if !strings.Contains(output, "service=test-service") {
		t.Errorf("Expected log output to contain service attribute, got: %s", output)
	}

	if !strings.Contains(output, "version=1.0.0") {
		t.Errorf("Expected log output to contain version attribute, got: %s", output)
	}
}

func TestTraceHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer

	baseHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	traceHandler := NewTraceHandler(baseHandler)

	// Add a group to the handler
	loggerWithGroup := slog.New(traceHandler.WithGroup("request"))

	loggerWithGroup.Info("test message", "method", "GET", "path", "/api/agents")

	output := buf.String()

	// Groups in text handler appear as dotted names
	if !strings.Contains(output, "request.method=GET") {
		t.Errorf("Expected log output to contain grouped attribute, got: %s", output)
	}
}
