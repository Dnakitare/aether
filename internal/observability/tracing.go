// Package observability provides distributed tracing, metrics, and logging.
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	serviceName    = "aether"
	serviceVersion = "0.1.0"
)

// TracerConfig holds configuration for distributed tracing.
type TracerConfig struct {
	// Enabled determines if tracing is active
	Enabled bool

	// Endpoint is the OTLP collector endpoint (e.g., "localhost:4317")
	Endpoint string

	// SamplingRatio is the fraction of traces to sample (0.0 to 1.0)
	SamplingRatio float64

	// Environment (development, staging, production)
	Environment string

	// UseStdout exports traces to stdout instead of OTLP (for development)
	UseStdout bool
}

// TracerProvider wraps OpenTelemetry tracer provider.
type TracerProvider struct {
	provider *sdktrace.TracerProvider
	logger   *slog.Logger
	config   TracerConfig
}

// NewTracerProvider creates a new tracer provider.
func NewTracerProvider(logger *slog.Logger, config TracerConfig) (*TracerProvider, error) {
	if !config.Enabled {
		logger.Info("tracing disabled")
		return &TracerProvider{
			provider: sdktrace.NewTracerProvider(),
			logger:   logger,
			config:   config,
		}, nil
	}

	// Create resource with service information
	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
			attribute.String("environment", config.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporter
	var exporter sdktrace.SpanExporter
	if config.UseStdout {
		exporter, err = stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout exporter: %w", err)
		}
		logger.Info("using stdout trace exporter")
	} else {
		// Create OTLP gRPC exporter
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		exporter, err = otlptrace.New(
			ctx,
			otlptracegrpc.NewClient(
				otlptracegrpc.WithEndpoint(config.Endpoint),
				otlptracegrpc.WithInsecure(), // TODO: Use TLS in production
			),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
		}
		logger.Info("using OTLP trace exporter", "endpoint", config.Endpoint)
	}

	// Create sampler based on sampling ratio
	var sampler sdktrace.Sampler
	if config.SamplingRatio >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if config.SamplingRatio <= 0.0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(config.SamplingRatio)
	}

	// Create tracer provider
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global tracer provider
	otel.SetTracerProvider(provider)

	// Set global propagator to trace context (W3C standard)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	logger.Info("tracing initialized",
		"sampling_ratio", config.SamplingRatio,
		"environment", config.Environment,
	)

	return &TracerProvider{
		provider: provider,
		logger:   logger,
		config:   config,
	}, nil
}

// Tracer returns a tracer for the given name.
func (tp *TracerProvider) Tracer(name string) trace.Tracer {
	return tp.provider.Tracer(name)
}

// Shutdown gracefully shuts down the tracer provider.
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp.provider == nil {
		return nil
	}

	tp.logger.Info("shutting down tracer provider")
	if err := tp.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown tracer provider: %w", err)
	}

	return nil
}

// GetLoggerWithTracing returns a logger that automatically includes trace context.
// The otelslog bridge automatically injects trace IDs into log records.
func GetLoggerWithTracing(name string) *slog.Logger {
	// Create a new handler with OpenTelemetry bridge
	// This will automatically inject trace_id and span_id into logs
	handler := otelslog.NewHandler(name)
	return slog.New(handler)
}

// SpanFromContext extracts the current span from context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// TraceIDFromContext extracts the trace ID from context.
func TraceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}
	return span.SpanContext().TraceID().String()
}

// SpanIDFromContext extracts the span ID from context.
func SpanIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}
	return span.SpanContext().SpanID().String()
}

// AddSpanAttributes adds attributes to the current span in context.
func AddSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

// AddSpanEvent adds an event to the current span in context.
func AddSpanEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.AddEvent(name, trace.WithAttributes(attrs...))
	}
}

// RecordError records an error on the current span in context.
func RecordError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.RecordError(err)
	}
}

// StartSpan is a convenience function to start a span with common attributes.
func StartSpan(ctx context.Context, tracer trace.Tracer, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := tracer.Start(ctx, name)
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	return ctx, span
}
