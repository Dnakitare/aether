package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// StartSpan starts a new span with the given name and options.
// It returns the span and a context with the span attached.
//
// Example usage:
//
//	ctx, span := tracing.StartSpan(ctx, "operation-name")
//	defer span.End()
func StartSpan(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return trace.SpanFromContext(ctx).TracerProvider().
		Tracer("aether").
		Start(ctx, spanName, opts...)
}

// RecordError records an error on the span and sets the span status to error.
func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SetAttributes sets multiple attributes on the span.
func SetAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	span.SetAttributes(attrs...)
}

// AddEvent adds an event to the span with optional attributes.
func AddEvent(span trace.Span, name string, attrs ...attribute.KeyValue) {
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// Common attribute keys for consistent naming
var (
	// Agent attributes
	AttrAgentID     = attribute.Key("aether.agent.id")
	AttrAgentName   = attribute.Key("aether.agent.name")
	AttrAgentStatus = attribute.Key("aether.agent.status")
	AttrAgentImage  = attribute.Key("aether.agent.image")

	// Tenant attributes
	AttrTenantID = attribute.Key("aether.tenant.id")

	// Scheduler attributes
	AttrSchedulerNodeID    = attribute.Key("aether.scheduler.node_id")
	AttrSchedulerStrategy  = attribute.Key("aether.scheduler.strategy")
	AttrSchedulerQueueSize = attribute.Key("aether.scheduler.queue_size")

	// VM attributes
	AttrVMID     = attribute.Key("aether.vm.id")
	AttrVMStatus = attribute.Key("aether.vm.status")

	// Resource attributes
	AttrResourceCPU    = attribute.Key("aether.resource.cpu_count")
	AttrResourceMemory = attribute.Key("aether.resource.memory_mb")
	AttrResourceDisk   = attribute.Key("aether.resource.disk_mb")

	// Operation attributes
	AttrOperationSuccess  = attribute.Key("aether.operation.success")
	AttrOperationDuration = attribute.Key("aether.operation.duration_ms")

	// Error attributes
	AttrErrorType    = attribute.Key("error.type")
	AttrErrorMessage = attribute.Key("error.message")
)
