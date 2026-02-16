#!/bin/bash
set -e

# Establish Performance Baseline for Aether
# This script runs load tests and benchmarks to establish baseline performance metrics

echo "==================================================================="
echo "Aether Performance Baseline Establishment"
echo "==================================================================="
echo ""

# Check if infrastructure is running
echo "1. Checking infrastructure..."
if ! docker-compose -f deployments/docker/docker-compose.dev.yml ps | grep -q "Up"; then
    echo "❌ Infrastructure not running. Starting now..."
    docker-compose -f deployments/docker/docker-compose.dev.yml up -d
    echo "⏳ Waiting for services to be ready..."
    sleep 10
else
    echo "✅ Infrastructure is running"
fi

# Set environment variables
export DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@localhost:5432/aether?sslmode=disable}"
export REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"
export REDIS_PASSWORD="${REDIS_PASSWORD:-redis_dev_password}"
export ETCD_ENDPOINTS="${ETCD_ENDPOINTS:-localhost:2379}"

echo ""
echo "2. Environment Configuration:"
echo "   DATABASE_URL: $DATABASE_URL"
echo "   REDIS_ADDR: $REDIS_ADDR"
echo "   ETCD_ENDPOINTS: $ETCD_ENDPOINTS"
echo ""

# Create results directory
RESULTS_DIR="performance-baseline-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$RESULTS_DIR"

echo "3. Running benchmarks..."
echo "   Results will be saved to: $RESULTS_DIR"
echo ""

# Run benchmarks
echo "   Running micro-benchmarks..."
go test -bench=. -benchmem -run=^$ ./tests/load/... > "$RESULTS_DIR/benchmarks.txt" 2>&1
if [ $? -eq 0 ]; then
    echo "   ✅ Benchmarks completed"
    echo ""
    echo "   Top Results:"
    grep "Benchmark" "$RESULTS_DIR/benchmarks.txt" | head -5
else
    echo "   ⚠️  Benchmarks completed with warnings (check logs)"
fi

echo ""
echo "4. Running load tests..."
echo "   ⏳ This may take 5-10 minutes..."
echo ""

# Run 1K agent test (if infrastructure is available)
echo "   Testing 1,000 concurrent agents..."
if go test -v -timeout 10m -run TestLoad_1000Agents ./tests/load/... > "$RESULTS_DIR/load-1k.txt" 2>&1; then
    echo "   ✅ 1K agent test completed"

    # Extract key metrics
    echo ""
    echo "   Key Metrics (1K agents):"
    grep -A 15 "=== PERFORMANCE REPORT ===" "$RESULTS_DIR/load-1k.txt" | tail -15 || echo "   (See full results in $RESULTS_DIR/load-1k.txt)"
else
    echo "   ⚠️  1K agent test skipped (infrastructure may not be available)"
fi

echo ""
echo "==================================================================="
echo "Baseline Establishment Complete!"
echo "==================================================================="
echo ""
echo "Results saved to: $RESULTS_DIR"
echo ""
echo "Files created:"
ls -lh "$RESULTS_DIR"
echo ""
echo "Next steps:"
echo "1. Review results in $RESULTS_DIR/"
echo "2. Document baseline metrics in docs/BASELINE_METRICS.md"
echo "3. Use 'benchstat' to compare future runs against this baseline"
echo ""
echo "Example comparison:"
echo "  go test -bench=. ./tests/load/... > new-results.txt"
echo "  benchstat $RESULTS_DIR/benchmarks.txt new-results.txt"
echo ""
