// Package tracing provides distributed tracing support using OpenTelemetry.
package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

// Config holds the configuration for the tracer.
type Config struct {
	// Enabled determines if tracing is active
	Enabled bool

	// ServiceName is the name of the service
	ServiceName string

	// ServiceVersion is the version of the service
	ServiceVersion string

	// Environment is the deployment environment (development, staging, production)
	Environment string

	// OTLPEndpoint is the OTLP collector endpoint (e.g., "localhost:4317")
	OTLPEndpoint string

	// SamplingRate is the probability of sampling (0.0 to 1.0)
	// 1.0 = sample everything, 0.1 = sample 10%
	SamplingRate float64
}

// Provider wraps the OpenTelemetry tracer provider.
type Provider struct {
	provider *sdktrace.TracerProvider
	config   Config
}

// NewProvider creates a new tracing provider with the given configuration.
func NewProvider(cfg Config) (*Provider, error) {
	if !cfg.Enabled {
		// Return a no-op provider
		return &Provider{
			provider: sdktrace.NewTracerProvider(),
			config:   cfg,
		}, nil
	}

	// Set defaults
	if cfg.ServiceName == "" {
		cfg.ServiceName = "aether"
	}
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = "v0.2.0-beta"
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if cfg.OTLPEndpoint == "" {
		cfg.OTLPEndpoint = "localhost:4317"
	}
	if cfg.SamplingRate == 0 {
		cfg.SamplingRate = 1.0 // Sample everything by default
	}

	// Create OTLP exporter
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(), // For local development
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource with service information
	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SamplingRate)),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Set global propagator for context propagation
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return &Provider{
		provider: tp,
		config:   cfg,
	}, nil
}

// Tracer returns a tracer for the given instrumentation name.
func (p *Provider) Tracer(instrumentationName string) trace.Tracer {
	return p.provider.Tracer(instrumentationName)
}

// Shutdown flushes any pending spans and shuts down the provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}

// ForceFlush forces the provider to flush any pending spans.
func (p *Provider) ForceFlush(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	return p.provider.ForceFlush(ctx)
}

// IsEnabled returns whether tracing is enabled.
func (p *Provider) IsEnabled() bool {
	return p.config.Enabled
}
