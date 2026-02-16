package observability

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TestTracePropagationThroughContext verifies that trace context propagates
// correctly when passed through function calls, and that child spans maintain
// the relationship to parent spans.
func TestTracePropagationThroughContext(t *testing.T) {
	// Set up tracer provider
	tp := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
	)
	defer tp.Shutdown(context.Background())
	otel.SetTracerProvider(tp)

	// Create a buffer to capture logs
	var logBuf bytes.Buffer
	baseHandler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	traceHandler := NewTraceHandler(baseHandler)
	logger := slog.New(traceHandler)

	// Simulate a request flow: API → Scheduler → Runtime
	tracer := otel.Tracer("aether.test")

	// Root span (simulating API server receiving request)
	ctx := context.Background()
	ctx, apiSpan := tracer.Start(ctx, "api.CreateAgent")
	apiTraceID := apiSpan.SpanContext().TraceID().String()
	apiSpanID := apiSpan.SpanContext().SpanID().String()

	logger.InfoContext(ctx, "API: creating agent")

	// Child span 1 (simulating scheduler)
	ctx, schedulerSpan := tracer.Start(ctx, "scheduler.ScheduleAgent")
	schedulerSpanID := schedulerSpan.SpanContext().SpanID().String()

	logger.InfoContext(ctx, "Scheduler: scheduling agent")

	// Child span 2 (simulating runtime)
	ctx, runtimeSpan := tracer.Start(ctx, "runtime.CreateAgent")
	runtimeSpanID := runtimeSpan.SpanContext().SpanID().String()

	logger.InfoContext(ctx, "Runtime: creating VM")

	// End spans in reverse order
	runtimeSpan.End()
	schedulerSpan.End()
	apiSpan.End()

	// Verify logs contain trace IDs
	logOutput := logBuf.String()
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")

	if len(lines) != 3 {
		t.Fatalf("Expected 3 log lines, got %d: %s", len(lines), logOutput)
	}

	// Verify all logs have the same trace ID (they're part of the same trace)
	for i, line := range lines {
		if !strings.Contains(line, "trace_id="+apiTraceID) {
			t.Errorf("Log line %d missing trace_id: %s", i, line)
		}
	}

	// Verify each log has the correct span ID for its context
	if !strings.Contains(lines[0], "span_id="+apiSpanID) {
		t.Errorf("API log missing correct span_id: %s", lines[0])
	}
	if !strings.Contains(lines[1], "span_id="+schedulerSpanID) {
		t.Errorf("Scheduler log missing correct span_id: %s", lines[1])
	}
	if !strings.Contains(lines[2], "span_id="+runtimeSpanID) {
		t.Errorf("Runtime log missing correct span_id: %s", lines[2])
	}

	// Verify trace ID is consistent across all spans
	if apiSpan.SpanContext().TraceID() != schedulerSpan.SpanContext().TraceID() {
		t.Error("Scheduler span has different trace ID than API span")
	}
	if schedulerSpan.SpanContext().TraceID() != runtimeSpan.SpanContext().TraceID() {
		t.Error("Runtime span has different trace ID than scheduler span")
	}

	t.Logf("✅ Trace propagation verified:")
	t.Logf("   Trace ID: %s (consistent across all spans)", apiTraceID)
	t.Logf("   API Span ID: %s", apiSpanID)
	t.Logf("   Scheduler Span ID: %s", schedulerSpanID)
	t.Logf("   Runtime Span ID: %s", runtimeSpanID)
}

// TestTraceContextIsolation verifies that separate requests get separate trace IDs
func TestTraceContextIsolation(t *testing.T) {
	tp := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
	)
	defer tp.Shutdown(context.Background())
	otel.SetTracerProvider(tp)

	tracer := otel.Tracer("aether.test")

	// Request 1
	ctx1, span1 := tracer.Start(context.Background(), "request1")
	traceID1 := span1.SpanContext().TraceID().String()
	span1.End()

	// Request 2
	ctx2, span2 := tracer.Start(context.Background(), "request2")
	traceID2 := span2.SpanContext().TraceID().String()
	span2.End()

	// Verify they have different trace IDs
	if traceID1 == traceID2 {
		t.Errorf("Expected different trace IDs for separate requests, got %s for both", traceID1)
	}

	// Verify contexts are independent
	span1FromCtx1 := oteltrace.SpanFromContext(ctx1)
	span2FromCtx2 := oteltrace.SpanFromContext(ctx2)

	if span1FromCtx1.SpanContext().TraceID() == span2FromCtx2.SpanContext().TraceID() {
		t.Error("Expected isolated trace contexts for separate requests")
	}

	t.Logf("✅ Trace isolation verified:")
	t.Logf("   Request 1 Trace ID: %s", traceID1)
	t.Logf("   Request 2 Trace ID: %s", traceID2)
}

// TestTraceContextPreservation verifies that context is preserved across goroutines
func TestTraceContextPreservation(t *testing.T) {
	tp := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
	)
	defer tp.Shutdown(context.Background())
	otel.SetTracerProvider(tp)

	tracer := otel.Tracer("aether.test")

	// Parent span
	ctx, parentSpan := tracer.Start(context.Background(), "parent")
	parentTraceID := parentSpan.SpanContext().TraceID().String()

	// Channel to receive trace ID from goroutine
	resultCh := make(chan string, 1)

	// Pass context to goroutine
	go func(ctx context.Context) {
		// Create child span in goroutine
		_, childSpan := tracer.Start(ctx, "child-in-goroutine")
		defer childSpan.End()

		resultCh <- childSpan.SpanContext().TraceID().String()
	}(ctx)

	// Get result from goroutine
	childTraceID := <-resultCh

	parentSpan.End()

	// Verify trace ID is preserved across goroutine boundary
	if parentTraceID != childTraceID {
		t.Errorf("Trace ID not preserved across goroutine: parent=%s, child=%s",
			parentTraceID, childTraceID)
	}

	t.Logf("✅ Trace context preserved across goroutine boundary:")
	t.Logf("   Trace ID: %s", parentTraceID)
}
