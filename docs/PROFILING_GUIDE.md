# Profiling Guide for Aether

This guide explains how to profile Aether for performance optimization.

## Quick Start

```bash
# Run load test with CPU and memory profiling
./scripts/profile-load-test.sh

# Analyze results
./scripts/analyze-profile.sh

# Or open interactive web UI
go tool pprof -http=:8080 profiles/*/cpu.prof
```

## Available Tools

### 1. Automated Profiling Script

`./scripts/profile-load-test.sh` - Comprehensive profiling automation

**Features:**
- Automatic infrastructure checking
- CPU + memory profiling by default
- Optional block, mutex, and goroutine profiling
- Auto-analysis of results
- Profile metadata tracking

**Usage:**
```bash
# Default: 1K agents with CPU + memory profiles
./scripts/profile-load-test.sh

# 5K agents test
./scripts/profile-load-test.sh -t TestLoad_5000Agents

# All profile types
./scripts/profile-load-test.sh --all

# CPU only
./scripts/profile-load-test.sh --cpu-only
```

### 2. Analysis Script

`./scripts/analyze-profile.sh` - Quick profile analysis

**Features:**
- Shows top functions by CPU usage
- Memory allocation analysis
- Blocking operations detection
- Test performance summary

**Usage:**
```bash
# Analyze most recent profile
./scripts/analyze-profile.sh

# Analyze specific profile directory
./scripts/analyze-profile.sh profiles/20260215-155326/
```

## Profile Types

### CPU Profile (`-cpuprofile`)

**What it shows:** Where the program spends CPU time

**Use when:**
- Code is running slowly
- Need to identify hot paths
- Looking for algorithmic improvements

**Analysis:**
```bash
go tool pprof cpu.prof
(pprof) top10        # Top 10 functions by CPU
(pprof) list FuncName # Show source code
(pprof) web          # Visualize call graph
```

**Key metrics:**
- `flat`: Time spent in function itself
- `cum`: Cumulative time (function + callees)
- Higher percentages = optimization targets

### Memory Profile (`-memprofile`)

**What it shows:** Memory allocations

**Use when:**
- High memory usage
- Suspecting memory leaks
- Too many allocations causing GC pressure

**Analysis:**
```bash
go tool pprof -alloc_space mem.prof  # Total allocations
(pprof) top10

go tool pprof -inuse_space mem.prof  # Current memory
(pprof) top10
```

**Key metrics:**
- `alloc_space`: Total bytes allocated
- `inuse_space`: Bytes currently in use
- `alloc_objects`: Number of allocations
- `inuse_objects`: Objects currently allocated

**Optimization targets:**
- Functions with high alloc_space but not in hot path
- Functions with many small allocations
- Unexpected large allocations

### Block Profile (`-blockprofile`)

**What it shows:** Where goroutines block waiting

**Use when:**
- Low throughput despite low CPU usage
- Suspecting lock contention
- Channel operations seem slow

**Analysis:**
```bash
go tool pprof block.prof
(pprof) top10
```

**Common blocking causes:**
- Mutex lock contention
- Channel send/receive blocking
- Network I/O waits
- Sleep operations

### Mutex Profile (`-mutexprofile`)

**What it shows:** Lock contention

**Use when:**
- Concurrent performance is poor
- Many goroutines competing for locks
- Scalability issues

**Analysis:**
```bash
go tool pprof mutex.prof
(pprof) top10
```

## Profiling Workflow

### 1. Establish Baseline

```bash
# Run with current code
./scripts/profile-load-test.sh
mv profiles/latest profiles/baseline
```

### 2. Make Changes

Modify code to improve performance

### 3. Profile Again

```bash
./scripts/profile-load-test.sh
```

### 4. Compare

```bash
# Compare CPU profiles
go tool pprof -base=profiles/baseline/cpu.prof profiles/latest/cpu.prof
(pprof) top10  # Shows delta

# Compare memory
go tool pprof -base=profiles/baseline/mem.prof profiles/latest/mem.prof
```

### 5. Validate

- Did hot functions improve?
- Did overall runtime decrease?
- Did memory usage go down?
- Are there any regressions?

## Common Optimization Patterns

### 1. Reduce Allocations

**Before:**
```go
func processItems(items []string) []Result {
    results := []Result{}  // Allocates with each append
    for _, item := range items {
        results = append(results, process(item))
    }
    return results
}
```

**After:**
```go
func processItems(items []string) []Result {
    results := make([]Result, 0, len(items))  // Pre-allocate
    for _, item := range items {
        results = append(results, process(item))
    }
    return results
}
```

### 2. Reuse Buffers

**Before:**
```go
func formatMany(items []Item) []string {
    var results []string
    for _, item := range items {
        results = append(results, item.String())  // Many allocations
    }
    return results
}
```

**After:**
```go
type Formatter struct {
    buf strings.Builder  // Reusable buffer
}

func (f *Formatter) formatMany(items []Item) []string {
    results := make([]string, 0, len(items))
    for _, item := range items {
        f.buf.Reset()
        item.WriteTo(&f.buf)
        results = append(results, f.buf.String())
    }
    return results
}
```

### 3. Avoid Lock Contention

**Before:**
```go
type Counter struct {
    mu sync.Mutex
    count int
}

func (c *Counter) Increment() {
    c.mu.Lock()
    c.count++
    c.mu.Unlock()
}
```

**After:**
```go
type Counter struct {
    count atomic.Int64  // Lock-free
}

func (c *Counter) Increment() {
    c.count.Add(1)
}
```

### 4. Batch Operations

**Before:**
```go
for _, item := range items {
    db.Save(ctx, item)  // N database calls
}
```

**After:**
```go
db.SaveBatch(ctx, items)  // 1 database call
```

## Interpreting Profile Results

### CPU Profile Interpretation

```
flat  flat%   sum%        cum   cum%
100ms 50.00% 50.00%      200ms 100%   function1
 50ms 25.00% 75.00%       50ms  25%   function2
 30ms 15.00% 90.00%       30ms  15%   function3
```

- **function1**: Takes 50% of total time, 100ms in itself
  - Priority: HIGH (hot function)
  - Action: Optimize or reduce calls

- **function2**: 25% of time, all in function2 itself
  - Priority: MEDIUM
  - Action: Check if necessary, optimize if hot path

- **function3**: 15% of time
  - Priority: LOW unless called very frequently

### Memory Profile Interpretation

```
flat  flat%   sum%        cum   cum%
5MB  50.00% 50.00%       8MB  80.00%   function1
2MB  20.00% 70.00%       2MB  20.00%   function2
```

- **function1**: Allocates 5MB directly, 8MB total
  - Check: Is allocation necessary?
  - Action: Reuse buffers, pre-allocate

- **function2**: Allocates 2MB, all in function2
  - Check: Can we reduce allocation size?
  - Action: Use sync.Pool or reduce object size

## Advanced Techniques

### 1. Continuous Profiling

```go
// Add to production code
import _ "net/http/pprof"

func main() {
    go func() {
        http.ListenAndServe("localhost:6060", nil)
    }()
    // ... rest of code
}
```

Access live profiles:
```bash
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
go tool pprof http://localhost:6060/debug/pprof/heap
```

### 2. Benchmark Profiling

```bash
go test -bench=. -cpuprofile=cpu.prof -memprofile=mem.prof
go tool pprof cpu.prof
```

### 3. Trace Analysis

For understanding goroutine behavior and scheduling:

```bash
go test -trace=trace.out
go tool trace trace.out
```

## Profiling Checklist

Before optimizing:
- [ ] Establish baseline with profiling
- [ ] Identify actual bottleneck (not guessing)
- [ ] Verify bottleneck is in application code (not external)
- [ ] Confirm optimization is worth the complexity

After optimizing:
- [ ] Profile again with same workload
- [ ] Compare with baseline
- [ ] Verify improvement is significant (>10%)
- [ ] Run benchmarks to confirm
- [ ] Check for regressions in other areas

## Common Pitfalls

### 1. Profiling Wrong Workload

❌ Profiling development workload
✅ Profile production-like workload

### 2. Micro-Optimizing

❌ Optimizing function that takes 0.1% of time
✅ Focus on functions taking >10% of time

### 3. Premature Optimization

❌ Optimizing before profiling
✅ Profile first, then optimize

### 4. Ignoring Allocations

❌ Only looking at CPU profile
✅ Check memory profile for GC pressure

### 5. Not Comparing

❌ Looking at absolute numbers only
✅ Compare before/after profiles

## Profiling Best Practices

1. **Profile in realistic conditions**
   - Use production-like data sizes
   - Simulate real load patterns
   - Include realistic network latency

2. **Profile multiple times**
   - Run 3-5 times to account for variance
   - Average the results
   - Look for consistency

3. **Profile the right duration**
   - Too short: May miss important patterns
   - Too long: Unnecessarily large profile files
   - Sweet spot: 30-60 seconds for most cases

4. **Use appropriate tools**
   - CPU profile: For speed issues
   - Memory profile: For memory issues
   - Block profile: For concurrency issues
   - Trace: For understanding scheduling

5. **Document findings**
   - Record baseline metrics
   - Note optimization made
   - Track improvement percentage
   - Keep profiles for comparison

## Troubleshooting

### Profile is empty or has few samples

**Cause**: Test runs too fast
**Solution**: Increase workload or duration

### Can't connect to pprof web UI

**Cause**: Port already in use or firewall
**Solution**: Use different port: `-http=:8081`

### Out of memory during profiling

**Cause**: Memory profile too large
**Solution**: Profile shorter duration or use sampling

### Profiles show mostly runtime code

**Cause**: Application spends time in Go runtime
**Solution**: This is normal for I/O-bound code, look at goroutine traces

## Resources

- [Go Blog: Profiling Go Programs](https://go.dev/blog/pprof)
- [Google pprof Documentation](https://github.com/google/pprof/blob/master/doc/README.md)
- [Profiling and Optimizing Go](https://www.ardanlabs.com/blog/2017/05/language-mechanics-on-stacks-and-pointers.html)
- [Go Performance Workshop](https://dave.cheney.net/high-performance-go-workshop/dotgo-paris.html)

## Next Steps

After profiling, consider:
1. Database query optimization (see PERFORMANCE_TESTING.md)
2. Connection pool tuning
3. Caching strategies
4. Algorithm improvements
5. Concurrency patterns
