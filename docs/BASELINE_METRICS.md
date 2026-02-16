# Performance Baseline Metrics

This document tracks baseline performance metrics for Aether to detect regressions over time.

## How to Use This Document

1. Run `./scripts/establish-baseline.sh` to generate baseline metrics
2. Record the results below for the current version
3. When making changes, compare new results against the baseline
4. Update baselines when intentional performance improvements are made

## Beta v0.2.0 Baseline (Week 3 - Day 15)

**Test Date**: February 15, 2026
**Git Commit**: beta/v0.2.0 (Week 3 baseline)
**Hardware**: Apple M2, 8 cores
**OS**: Darwin 25.3.0 (macOS)

### Benchmark Results

Run: `make bench`

```
goos: darwin
goarch: arm64
pkg: github.com/aether-runtime/aether/tests/load
cpu: Apple M2

BenchmarkSchedulerPlacement-8              	 5247812	       221.8 ns/op	     272 B/op	       5 allocs/op
BenchmarkSchedulerPlacement_BinPacking-8   	 1000000	      1163 ns/op	     272 B/op	       5 allocs/op
BenchmarkSchedulerPlacement_Spread-8       	 1000000	      1158 ns/op	     272 B/op	       5 allocs/op
BenchmarkNodeCanFit-8                      	96183386	        11.79 ns/op	       0 B/op	       0 allocs/op
BenchmarkAgentRequestCreation-8            	 5426608	       229.6 ns/op	     272 B/op	       5 allocs/op
BenchmarkConcurrentPlacement-8             	 8100802	       138.1 ns/op	     272 B/op	       5 allocs/op
BenchmarkResourceCalculation-8             	1000000000	         0.30 ns/op	       0 B/op	       0 allocs/op
BenchmarkScheduler_1000Nodes-8             	 5122567	       229.6 ns/op	     272 B/op	       5 allocs/op
BenchmarkScheduler_WithTenantIsolation-8   	 3954121	       306.0 ns/op	     280 B/op	       6 allocs/op
BenchmarkSchedulerStats-8                  	38260578	        31.41 ns/op	       0 B/op	       0 allocs/op
```

**Key Insights:**
- **NodeCanFit**: Extremely fast at ~12ns per operation (zero allocations)
- **SchedulerPlacement**: Simple placement is ~222ns, very efficient
- **BinPacking/Spread**: More complex strategies ~1.2µs (acceptable trade-off)
- **ConcurrentPlacement**: Excellent scaling at 138ns per op
- **ResourceCalculation**: Nearly instantaneous at 0.3ns
- **Stats Collection**: Very efficient at 31ns for 100 nodes

Key benchmarks to track:
- `BenchmarkSchedulerPlacement`: Core scheduling algorithm speed (✅ 222ns)
- `BenchmarkNodeCanFit`: Resource checking performance (✅ 12ns)
- `BenchmarkConcurrentPlacement`: Multi-threaded performance (✅ 138ns)
- `BenchmarkScheduler_1000Nodes`: Large-scale scheduling (✅ 230ns)

### Load Test Results (1,000 Agents)

Run: `make load-test-1k`

```
Expected Results:
- Duration: 60-120s
- Throughput: 100+ ops/sec
- Success Rate: >99%
- Average Latency: <50ms
- P95 Latency: <100ms
- P99 Latency: <200ms

Actual Results:
TBD - Run ./scripts/establish-baseline.sh to populate
```

### Load Test Results (5,000 Agents)

Run: `make load-test-5k`

```
Expected Results:
- Duration: 180-300s
- Throughput: 50+ ops/sec
- Success Rate: >99%
- Average Latency: <100ms
- P95 Latency: <500ms
- P99 Latency: <1000ms

Actual Results:
TBD - Infrastructure test (run manually)
```

## Performance Targets vs Actuals

| Metric | Target | Baseline | Status |
|--------|--------|----------|--------|
| API Latency (avg) | <50ms | TBD | 🔶 Pending |
| API Latency (p95) | <100ms | TBD | 🔶 Pending |
| Agent Creation (avg) | <1s | TBD | 🔶 Pending |
| Agent Creation (p95) | <2s | TBD | 🔶 Pending |
| Scheduler Placement (avg) | <200ms | TBD | 🔶 Pending |
| Scheduler Placement (p95) | <500ms | TBD | 🔶 Pending |
| Throughput | 100 ops/sec | TBD | 🔶 Pending |
| 1K Concurrent Agents | Supported | TBD | 🔶 Pending |

**Legend:**
- ✅ Meets or exceeds target
- ⚠️ Within 10% of target
- ❌ Below target (needs optimization)
- 🔶 Not yet measured

## Regression Threshold

Performance is considered regressed if:
- Latency increases by >10% without justification
- Throughput decreases by >10% without justification
- Memory usage increases by >20% without justification

## Historical Baselines

### Alpha v0.1.0

No formal performance testing conducted. Load testing framework established in Beta.

## Notes

### Running Baseline Tests

```bash
# Ensure infrastructure is running
docker-compose -f deployments/docker/docker-compose.dev.yml up -d

# Run baseline establishment script
./scripts/establish-baseline.sh

# Results will be saved to: performance-baseline-YYYYMMDD-HHMMSS/
```

### Comparing Results

```bash
# After making changes
go test -bench=. ./tests/load/... > new-benchmarks.txt

# Compare with baseline
benchstat performance-baseline-*/benchmarks.txt new-benchmarks.txt
```

### When to Update Baseline

Update the baseline when:
1. Significant performance improvements are made intentionally
2. Major version releases (v0.2.0, v0.3.0, etc.)
3. Infrastructure changes (PostgreSQL upgrade, etc.)
4. After optimization sprints

Do NOT update baseline to hide regressions!

## Next Steps

- [ ] Run `./scripts/establish-baseline.sh` to collect initial metrics
- [ ] Document hardware specifications
- [ ] Record Git commit hash
- [ ] Update this document with actual results
- [ ] Set up CI/CD to run benchmarks on each PR
- [ ] Configure alerts for performance regressions
