# Distributed Tracing with OpenTelemetry

Aether uses OpenTelemetry for distributed tracing to provide visibility into agent lifecycle operations, scheduling decisions, and API requests.

## Quick Start

### 1. Start Jaeger (Local Development)

```bash
# Start all infrastructure including Jaeger
cd deployments/docker
docker-compose -f docker-compose.dev.yml up -d

# Verify Jaeger is running
curl http://localhost:16686/
```

### 2. Configure Tracing

Tracing is configured via the API server config:

```go
import "github.com/aether-runtime/aether/internal/observability"

tracerConfig := &observability.TracerConfig{
    Enabled:       true,
    Endpoint:      "localhost:4317",  // Jaeger OTLP endpoint
    SamplingRatio: 1.0,               // Sample 100% of traces (dev)
    Environment:   "development",
    UseStdout:     false,             // Use OTLP, not stdout
    UseTLS:        false,             // No TLS for local dev
}
```

### 3. View Traces

Open Jaeger UI: http://localhost:16686

- **Service**: `aether`
- **Operations**: `scheduler.ScheduleAgent`, `runtime.CreateAgent`, `GET /v1/agents`, etc.

## Log Correlation

All logs automatically include `trace_id` and `span_id` when operations are traced, enabling correlation between logs and traces.

### Example Log Output

```
level=INFO msg="creating agent" trace_id=4bf92f3577b34da6a3ce929d0e0e4736 span_id=00f067aa0ba902b7 agent_id=agent-123 tenant_id=tenant-abc
```

### Viewing Correlated Logs

1. **Find trace in Jaeger** → Copy the trace ID
2. **Search logs** for that trace ID:
   ```bash
   # Example: grep for trace ID
   grep "trace_id=4bf92f3577b34da6a3ce929d0e0e4736" /var/log/aether.log
   ```

3. **In production log aggregators** (Datadog, Splunk, etc.):
   ```
   trace_id:"4bf92f3577b34da6a3ce929d0e0e4736"
   ```

This allows you to see all logs related to a specific request, even across multiple services.

## Architecture

### Components

```
┌─────────────┐
│  API Server │ ──┐
└─────────────┘   │
                  │  OTLP gRPC
┌─────────────┐   │  (port 4317)
│  Scheduler  │ ──┼──────────► ┌─────────┐
└─────────────┘   │            │ Jaeger  │
                  │            └─────────┘
┌─────────────┐   │                 │
│   Runtime   │ ──┘                 │
└─────────────┘              View traces at
                             http://localhost:16686
```

### Trace Propagation

Traces propagate through the system using W3C Trace Context:

1. **HTTP Request** → API server creates root span
2. **Scheduler** → Creates child span for scheduling logic
3. **Runtime** → Creates child span for agent creation
4. **VM Manager** → Inherits context from runtime

Each component adds relevant attributes to enrich the trace.

## Instrumented Operations

### API Server (HTTP Middleware)

**All HTTP requests** are automatically traced:

```
Span: GET /v1/agents
├── http.method: GET
├── http.url: /v1/agents
├── http.status_code: 200
└── http.user_agent: curl/7.64.1
```

### Scheduler

**ScheduleAgent**: When an agent is queued for scheduling

```
Span: scheduler.ScheduleAgent
├── agent.id: agent-123
├── agent.tenant_id: tenant-abc
├── agent.cpu_cores: 2
├── agent.memory_mb: 512
└── queue.length: 5
```

**scheduleNext**: Core scheduling decision logic

```
Span: scheduler.scheduleNext
├── agent.id: agent-123
├── queue.length: 4
├── nodes.available: 3
├── node.id: node-1
├── node.utilization_before: 45.2
├── node.utilization_after: 67.8
└── success: true
```

### Runtime

**CreateAgent**: Agent lifecycle creation

```
Span: runtime.CreateAgent
├── agent.id: agent-123
├── agent.tenant_id: tenant-abc
├── agent.image: ubuntu:22.04
├── agent.cpu_count: 2
├── agent.memory_mb: 512
├── runtime.agent_count: 15
├── Events:
│   ├── creating_vm
│   ├── vm_created
│   └── persisting_to_state_store
```

## Implementation Details

### Automatic Trace ID Injection

The `TraceHandler` in `internal/observability/trace_handler.go` automatically extracts trace context from the context and adds it to log records:

```go
// When you log with a traced context:
logger.InfoContext(ctx, "operation started")

// The TraceHandler automatically adds:
// - trace_id: extracted from span context
// - span_id: extracted from span context
```

**No manual work required** - just use `*Context` logging methods:
- `logger.InfoContext(ctx, ...)`
- `logger.ErrorContext(ctx, ...)`
- `logger.DebugContext(ctx, ...)`

**DON'T use non-context methods** if you want trace correlation:
```go
// ❌ Wrong - no trace context
logger.Info("message")

// ✅ Correct - trace context included
logger.InfoContext(ctx, "message")
```

## Custom Instrumentation

### Adding Spans to Your Code

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
)

func MyOperation(ctx context.Context) error {
    tracer := otel.Tracer("aether.mycomponent")
    ctx, span := tracer.Start(ctx, "MyOperation",
        trace.WithAttributes(
            attribute.String("custom.field", "value"),
        ),
    )
    defer span.End()

    // Your code here...

    // Add events
    span.AddEvent("checkpoint_reached")

    // Record errors
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, "operation failed")
        return err
    }

    return nil
}
```

### Semantic Attributes

Use consistent attribute naming for better trace analysis:

| Component | Attribute Pattern | Example |
|-----------|------------------|---------|
| Agent | `agent.*` | `agent.id`, `agent.tenant_id`, `agent.image` |
| Scheduler | `scheduler.*`, `node.*` | `scheduler.strategy`, `node.id`, `node.utilization` |
| Runtime | `runtime.*` | `runtime.agent_count` |
| VM | `vm.*` | `vm.id`, `vm.status` |
| HTTP | `http.*` | `http.method`, `http.status_code` |

See `internal/observability/tracing.go` for helper functions.

## Production Configuration

### Sampling

For production, reduce sampling to avoid overhead:

```go
tracerConfig := &observability.TracerConfig{
    Enabled:       true,
    Endpoint:      "otel-collector:4317",
    SamplingRatio: 0.1,  // Sample 10% of traces
    Environment:   "production",
    UseTLS:        true,
}
```

### Exporters

Jaeger is for local development. For production, consider:

- **Jaeger** (self-hosted)
- **Tempo** (Grafana Cloud)
- **AWS X-Ray** (via OTLP bridge)
- **Google Cloud Trace**
- **Datadog APM**

All work with OpenTelemetry's OTLP exporter.

## Troubleshooting

### No Traces Appearing

1. Check Jaeger is running:
   ```bash
   docker ps | grep jaeger
   curl http://localhost:16686/
   ```

2. Check tracing is enabled:
   ```bash
   # In server config
   tracerConfig.Enabled = true
   ```

3. Check OTLP endpoint is correct:
   ```bash
   # Default for local Jaeger
   tracerConfig.Endpoint = "localhost:4317"
   ```

4. Check logs for tracer initialization:
   ```
   level=INFO msg="tracing initialized" sampling_ratio=1.0 environment=development
   ```

### Traces Missing Attributes

Make sure context is propagated:

```go
// ❌ Wrong - creates new background context
ctx := context.Background()

// ✅ Correct - uses context from caller
func MyFunc(ctx context.Context) {
    ctx, span := tracer.Start(ctx, "MyFunc")
    defer span.End()

    // Pass ctx to children
    childFunc(ctx)
}
```

### High Overhead

1. Reduce sampling ratio in production
2. Avoid creating too many spans (> 1000 per request)
3. Use span events instead of nested spans for fine-grained tracking

## References

- [OpenTelemetry Go Docs](https://opentelemetry.io/docs/instrumentation/go/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)
- [W3C Trace Context](https://www.w3.org/TR/trace-context/)
- [Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/)

## Example Queries

### Find slow scheduling operations

In Jaeger UI:
- Service: `aether`
- Operation: `scheduler.scheduleNext`
- Min Duration: `500ms`

### Find failed agent creations

In Jaeger UI:
- Service: `aether`
- Operation: `runtime.CreateAgent`
- Tags: `error=true`

### Trace a specific agent

In Jaeger UI:
- Service: `aether`
- Tags: `agent.id=agent-123`
