# Load Testing Framework

This directory contains load and performance tests for Aether.

## Overview

The load testing framework validates Aether's performance targets:
- **API Latency**: <100ms (p95)
- **Agent Creation Time**: <2s (p95)
- **Scheduler Placement**: <500ms (p95)
- **Throughput**: Support 1,000+ concurrent agents

## Test Types

### 1. Load Tests (`*_load_test.go`)
- Test system behavior under realistic load
- Scenarios: 1K, 5K, 10K agents
- Measure throughput, latency, resource utilization

### 2. Benchmarks (`*_bench_test.go`)
- Go benchmark tests for component performance
- Used for micro-optimizations and regression detection
- Run with `go test -bench=.`

### 3. Stress Tests (`*_stress_test.go`)
- Push system beyond normal capacity
- Identify breaking points
- Test degradation behavior

## Running Tests

```bash
# Run all load tests
make load-test

# Run specific scenario
go test -v ./tests/load -run TestLoad_1000Agents

# Run benchmarks
go test -bench=. ./tests/load

# Run with profiling
go test -v ./tests/load -run TestLoad_1000Agents -cpuprofile=cpu.prof -memprofile=mem.prof

# Analyze profiles
go tool pprof cpu.prof
go tool pprof mem.prof
```

## Test Scenarios

### Scenario 1: 1,000 Agents
- **Goal**: Baseline performance validation
- **Duration**: ~2 minutes
- **Metrics**: Throughput, avg/p95/p99 latency

### Scenario 2: 5,000 Agents
- **Goal**: High-load validation
- **Duration**: ~10 minutes
- **Metrics**: System stress, resource saturation

### Scenario 3: Sustained Load
- **Goal**: Stability over time
- **Duration**: 30 minutes
- **Metrics**: Memory leaks, performance degradation

## Performance Targets

| Metric | Target | P95 | P99 |
|--------|--------|-----|-----|
| API Request | <50ms | <100ms | <200ms |
| Agent Creation | <1s | <2s | <5s |
| Scheduler Placement | <200ms | <500ms | <1s |
| Throughput | 100 agents/sec | - | - |

## Prerequisites

```bash
# Start infrastructure
docker-compose -f deployments/docker/docker-compose.dev.yml up -d

# Wait for services to be ready
sleep 5

# Set environment variables
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/aether?sslmode=disable"
export REDIS_ADDR="localhost:6379"
export REDIS_PASSWORD="redis_dev_password"
```

## Analyzing Results

Load tests generate JSON output with detailed metrics:

```json
{
  "scenario": "1000_agents",
  "timestamp": "2026-02-15T10:00:00Z",
  "duration": "120s",
  "total_requests": 1000,
  "successful": 998,
  "failed": 2,
  "throughput": 8.33,
  "latency": {
    "avg": "45ms",
    "p50": "42ms",
    "p95": "89ms",
    "p99": "156ms"
  }
}
```

## Troubleshooting

### Tests Fail with "Connection refused"
- Ensure infrastructure is running: `docker-compose ps`
- Check services are healthy: `docker-compose logs`

### Low Throughput
- Check CPU/memory availability
- Verify no other tests running
- Check database connection pool settings

### High Latency
- Profile the code: `go test -cpuprofile=cpu.prof`
- Check for lock contention
- Verify database query performance

## Contributing

When adding new load tests:
1. Follow the existing test structure
2. Include performance targets in test assertions
3. Add scenario documentation to this README
4. Update the Makefile if needed
