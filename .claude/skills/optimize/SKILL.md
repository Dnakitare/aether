---
name: optimize
description: Performance profiling and improvement. Use when code is slow, memory usage is high, or performance needs improvement.
hooks:
  post-tool-use:
    - matcher: "Edit|Write"
      command: ".claude/hooks/validators/test-runner.sh"
      timeout: 120
---

# Optimize

Profile and improve performance. Measure first, optimize second.

## Usage

```
/optimize [what to optimize]
```

## Examples

```
/optimize slow API endpoint /api/users
/optimize database queries taking too long
/optimize React component re-renders
/optimize memory usage in data processing
/optimize build time
```

## Philosophy

- **Measure First** - Don't guess at bottlenecks
- **Profile Before Optimizing** - Find the real problem
- **80/20 Rule** - Focus on biggest impact
- **Readability Matters** - Don't sacrifice clarity for micro-gains

## Process

1. **Measure Current State**
   - How slow is it actually?
   - What's the baseline?
   - What's the target?

2. **Profile**
   - Where is time being spent?
   - What's the hot path?
   - What's the bottleneck?

3. **Identify Opportunities**
   - What can be cached?
   - What can be parallelized?
   - What can be eliminated?

4. **Implement Fix**
   - Make one change at a time
   - Measure after each change
   - Keep the improvement

5. **Verify**
   - Did it actually improve?
   - Any regressions?
   - Is it still correct?

## Common Optimizations

### Database
- Add missing indexes
- Reduce N+1 queries
- Use pagination
- Cache frequent queries

### API
- Add caching headers
- Compress responses
- Reduce payload size
- Batch requests

### Frontend
- Lazy load components
- Memoize expensive calculations
- Virtualize long lists
- Optimize images

### General
- Cache expensive operations
- Use appropriate data structures
- Avoid unnecessary work
- Parallelize when possible

## Quick Wins

| Area | Quick Check |
|------|-------------|
| Database | `EXPLAIN ANALYZE` on slow queries |
| Node.js | `--prof` flag, clinic.js |
| Python | cProfile, line_profiler |
| React | React DevTools Profiler |
| Network | Browser DevTools Network tab |

## Output Format

```markdown
## Optimization: [What]

### Current Performance
- [Metric]: [Value]

### Bottleneck Found
[What's causing slowness]

### Changes Made
1. [Change 1] - [Impact]
2. [Change 2] - [Impact]

### Results
- Before: [metric]
- After: [metric]
- Improvement: [X%]

### Trade-offs
[Any downsides to the optimization]
```
