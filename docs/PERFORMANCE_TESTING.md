# Performance Testing Guide

This guide explains how to run performance tests and interpret results for Aether.

## Overview

Aether's performance testing framework consists of:
1. **Load Tests**: Test system behavior under realistic load (1K, 5K agents)
2. **Benchmarks**: Micro-benchmarks for component performance
3. **Stress Tests**: Push system beyond normal capacity
4. **Sustained Tests**: Long-running tests to detect memory leaks

## Performance Targets

### Beta v0.2.0 Targets

| Metric | Target | P95 | P99 |
|--------|--------|-----|-----|
| API Request Latency | <50ms | <100ms | <200ms |
| Agent Creation Time | <1s | <2s | <5s |
| Scheduler Placement | <200ms | <500ms | <1s |
| Throughput | 100 agents/sec | - | - |
| Concurrent Agents | 1,000+ | - | - |

## Prerequisites

### 1. Infrastructure Setup

Start the required services:

```bash
# Start PostgreSQL, Redis, etcd, Kafka, Jaeger
docker-compose -f deployments/docker/docker-compose.dev.yml up -d

# Verify services are healthy
docker-compose -f deployments/docker/docker-compose.dev.yml ps

# Wait for services to be ready (especially PostgreSQL)
sleep 5
```

### 2. Environment Variables

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/aether?sslmode=disable"
export REDIS_ADDR="localhost:6379"
export REDIS_PASSWORD="redis_dev_password"
export ETCD_ENDPOINTS="localhost:2379"
export KAFKA_BROKERS="localhost:9092"
```

### 3. System Resources

For optimal results:
- **CPU**: 4+ cores recommended
- **RAM**: 8GB+ available
- **Disk**: SSD preferred for PostgreSQL

## Running Tests

### Quick Start

```bash
# Run all load tests (1K + 5K agents)
make load-test

# Run specific scenarios
make load-test-1k    # 1,000 agents (~5 minutes)
make load-test-5k    # 5,000 agents (~15 minutes)

# Run benchmarks
make bench

# Run benchmarks with report
make bench-report
```

### Detailed Usage

#### 1. Load Tests

```bash
# 1,000 agent load test
go test -v -timeout 10m -run TestLoad_1000Agents ./tests/load/

# 5,000 agent load test
go test -v -timeout 20m -run TestLoad_5000Agents ./tests/load/

# Run with profiling
go test -v -run TestLoad_1000Agents ./tests/load/ \
  -cpuprofile=cpu.prof \
  -memprofile=mem.prof
```

#### 2. Benchmarks

```bash
# Run all benchmarks
go test -bench=. -benchmem ./tests/load/

# Run specific benchmark
go test -bench=BenchmarkSchedulerPlacement -benchmem ./tests/load/

# Save benchmark results
go test -bench=. -benchmem ./tests/load/ > baseline.txt

# Compare with baseline
go test -bench=. -benchmem ./tests/load/ > current.txt
benchstat baseline.txt current.txt
```

#### 3. Profiling

```bash
# CPU profiling
go test -run TestLoad_1000Agents ./tests/load/ -cpuprofile=cpu.prof
go tool pprof cpu.prof

# Memory profiling
go test -run TestLoad_1000Agents ./tests/load/ -memprofile=mem.prof
go tool pprof mem.prof

# Interactive profiling
(pprof) top10        # Top 10 functions by CPU/memory
(pprof) list funcName # Show source code for function
(pprof) web          # Generate call graph (requires graphviz)
```

## Understanding Results

### Load Test Output

```
=== PERFORMANCE REPORT ===
Duration:              120.5s
Total Requests:        1000
Successful:            998
Failed:                2
Success Rate:          99.80%
Throughput:            8.28 ops/sec

Latency Statistics:
  Average:             45ms
  P50:                 42ms
  P95:                 89ms
  P99:                 156ms
  Avg Enqueue:         2ms
  Avg Placement:       43ms
========================
```

**Key Metrics:**
- **Throughput**: Operations per second (higher is better)
- **Success Rate**: Percentage of successful operations (target: >99%)
- **P95/P99 Latency**: 95th/99th percentile latency (most users experience this)
- **Average Latency**: Mean latency across all operations

### Benchmark Output

```
BenchmarkSchedulerPlacement-8           1000000    1234 ns/op    512 B/op    8 allocs/op
BenchmarkNodeCanFit-8                  100000000     12 ns/op      0 B/op    0 allocs/op
```

**Columns:**
- **Name-N**: Benchmark name and GOMAXPROCS
- **Iterations**: Number of times benchmark ran
- **ns/op**: Nanoseconds per operation (lower is better)
- **B/op**: Bytes allocated per operation (lower is better)
- **allocs/op**: Allocations per operation (lower is better)

## Performance Regression Detection

### 1. Establish Baseline

Run tests on known-good commit:

```bash
git checkout v0.2.0
make bench-report
mv bench-results.txt bench-baseline.txt
```

### 2. Compare After Changes

```bash
# After making changes
make bench-report

# Compare
benchstat bench-baseline.txt bench-results.txt
```

### 3. Interpret Results

```
name                  old time/op  new time/op  delta
SchedulerPlacement-8  1.23µs ± 2%  1.45µs ± 3%  +17.89% (p=0.000)
```

- **+17.89%**: Performance regression (slower)
- **-15.23%**: Performance improvement (faster)
- **(p=0.000)**: Statistically significant change

## Troubleshooting

### Tests Fail with "Connection refused"

**Cause**: Infrastructure services not running

**Fix**:
```bash
docker-compose -f deployments/docker/docker-compose.dev.yml up -d
docker-compose -f deployments/docker/docker-compose.dev.yml ps
```

### Low Throughput (<50 ops/sec)

**Possible Causes**:
1. Insufficient system resources (CPU, RAM)
2. Other processes consuming resources
3. Database connection pool too small
4. Network latency to services

**Debug**:
```bash
# Check system resources
top
htop

# Check Docker resource usage
docker stats

# Profile the test
go test -run TestLoad_1000Agents ./tests/load/ -cpuprofile=cpu.prof
go tool pprof -http=:8080 cpu.prof
```

### High Latency (P95 >500ms)

**Possible Causes**:
1. Database query performance issues
2. Lock contention
3. Too many concurrent operations
4. Insufficient worker goroutines

**Debug**:
```bash
# Check PostgreSQL slow queries
docker-compose logs postgres | grep "duration"

# Check Redis performance
redis-cli --latency

# Profile for blocking
go test -run TestLoad_1000Agents ./tests/load/ -blockprofile=block.prof
go tool pprof block.prof
```

### Memory Usage Growing

**Possible Causes**:
1. Memory leak
2. Goroutine leak
3. Unbounded caches
4. Missing cleanup

**Debug**:
```bash
# Memory profiling
go test -run TestLoad_1000Agents ./tests/load/ -memprofile=mem.prof
go tool pprof -alloc_space mem.prof

# Check goroutines
go tool pprof -http=:8080 goroutine.prof
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Performance Tests

on:
  push:
    branches: [main, beta/*]
  pull_request:
    branches: [main]

jobs:
  load-test:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Start infrastructure
        run: docker-compose -f deployments/docker/docker-compose.dev.yml up -d

      - name: Wait for services
        run: sleep 10

      - name: Run 1K load test
        run: make load-test-1k

      - name: Run benchmarks
        run: make bench-report

      - name: Upload results
        uses: actions/upload-artifact@v3
        with:
          name: performance-results
          path: bench-results.txt
```

## Best Practices

### 1. Run Tests Consistently
- Same hardware/environment
- Close other applications
- Run multiple times and average results
- Use dedicated test infrastructure for official benchmarks

### 2. Establish Baselines
- Record performance metrics for each release
- Track trends over time
- Set regression thresholds

### 3. Profile Before Optimizing
- Identify bottlenecks with profiling
- Focus on hot paths (top 10 functions)
- Don't optimize prematurely

### 4. Test Realistic Scenarios
- Use production-like data sizes
- Simulate realistic access patterns
- Include failure scenarios

### 5. Monitor During Tests
- Watch system metrics (CPU, RAM, disk I/O)
- Monitor application metrics (via Prometheus)
- Check for errors in logs

## Performance Optimization Checklist

When optimizing for performance:

- [ ] Profile to identify bottlenecks (CPU, memory, blocking)
- [ ] Check database query performance (use EXPLAIN)
- [ ] Verify connection pool sizes are appropriate
- [ ] Look for N+1 query problems
- [ ] Check for excessive allocations
- [ ] Review lock contention (use -blockprofile)
- [ ] Verify goroutine counts aren't growing unbounded
- [ ] Check for unnecessary serialization/deserialization
- [ ] Review caching strategy
- [ ] Validate network round trips are minimized

## Additional Resources

- [Go Performance Best Practices](https://github.com/dgryski/go-perfbook)
- [Profiling Go Programs](https://go.dev/blog/pprof)
- [Database Performance Tuning](../architecture/DATABASE.md)
- [Load Testing Guide](../tests/load/README.md)

## Reporting Performance Issues

When reporting performance issues, include:

1. **Test command used**
2. **System specifications** (CPU, RAM, OS)
3. **Infrastructure versions** (PostgreSQL, Redis, etc.)
4. **Performance report output**
5. **Profile data** (if available)
6. **Expected vs actual results**

Example:
```
Test: TestLoad_1000Agents
System: 8-core, 16GB RAM, Ubuntu 22.04
PostgreSQL: 15.2, Redis: 7.0

Expected: 100 ops/sec, P95 < 100ms
Actual: 45 ops/sec, P95 = 245ms

Profile: cpu.prof attached
```
