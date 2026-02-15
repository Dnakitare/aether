// Package observability provides logging with trace context.
package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceHandler is a slog.Handler that automatically adds trace context to logs.
type TraceHandler struct {
	base slog.Handler
}

// NewTraceHandler creates a new handler that adds trace IDs to log records.
func NewTraceHandler(base slog.Handler) *TraceHandler {
	return &TraceHandler{base: base}
}

// Enabled reports whether the handler handles records at the given level.
func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

// Handle adds trace context to the record and passes it to the base handler.
func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	// Extract trace context from the context
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		// Add trace_id and span_id to the log record
		r.AddAttrs(
			slog.String("trace_id", span.SpanContext().TraceID().String()),
			slog.String("span_id", span.SpanContext().SpanID().String()),
		)
	}

	return h.base.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes added.
func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{base: h.base.WithAttrs(attrs)}
}

// WithGroup returns a new handler with the given group added.
func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{base: h.base.WithGroup(name)}
}
