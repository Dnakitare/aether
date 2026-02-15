#!/bin/bash
set -e

# Profile Load Tests - Generate CPU and Memory Profiles
# Usage: ./scripts/profile-load-test.sh [test-name] [options]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PROFILES_DIR="$PROJECT_ROOT/profiles/$(date +%Y%m%d-%H%M%S)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
TEST_NAME="TestLoad_1000Agents"
PROFILE_CPU=true
PROFILE_MEM=true
PROFILE_BLOCK=false
PROFILE_MUTEX=false
PROFILE_GOROUTINE=false
AUTO_ANALYZE=true
TIMEOUT="15m"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--test)
            TEST_NAME="$2"
            shift 2
            ;;
        --cpu-only)
            PROFILE_MEM=false
            shift
            ;;
        --mem-only)
            PROFILE_CPU=false
            shift
            ;;
        --all)
            PROFILE_BLOCK=true
            PROFILE_MUTEX=true
            PROFILE_GOROUTINE=true
            shift
            ;;
        --no-analyze)
            AUTO_ANALYZE=false
            shift
            ;;
        --timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        -h|--help)
            cat <<EOF
Usage: $0 [options]

Profile load tests and generate pprof data for analysis.

Options:
    -t, --test NAME        Test to run (default: TestLoad_1000Agents)
    --cpu-only            Only CPU profiling
    --mem-only            Only memory profiling
    --all                 Enable all profiling types
    --no-analyze          Don't auto-analyze profiles
    --timeout DURATION    Test timeout (default: 15m)
    -h, --help            Show this help

Examples:
    $0                                    # Profile 1K test with CPU+mem
    $0 -t TestLoad_5000Agents --all      # Profile 5K test with all profiles
    $0 --cpu-only --no-analyze           # CPU profile only, no analysis

Available tests:
    TestLoad_1000Agents    1,000 agents (default)
    TestLoad_5000Agents    5,000 agents (requires infrastructure)

EOF
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

echo -e "${BLUE}==================================================================${NC}"
echo -e "${BLUE}Aether Load Test Profiling${NC}"
echo -e "${BLUE}==================================================================${NC}"
echo ""

# Check infrastructure
echo -e "${YELLOW}1. Checking infrastructure...${NC}"
if ! docker-compose -f deployments/docker/docker-compose.dev.yml ps 2>/dev/null | grep -q "Up"; then
    echo -e "${RED}❌ Infrastructure not running${NC}"
    echo -e "${YELLOW}Starting infrastructure...${NC}"
    docker-compose -f deployments/docker/docker-compose.dev.yml up -d
    echo -e "${YELLOW}⏳ Waiting for services...${NC}"
    sleep 10
fi
echo -e "${GREEN}✅ Infrastructure ready${NC}"
echo ""

# Set environment variables
export DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@localhost:5432/aether?sslmode=disable}"
export REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"
export REDIS_PASSWORD="${REDIS_PASSWORD:-redis_dev_password}"
export ETCD_ENDPOINTS="${ETCD_ENDPOINTS:-localhost:2379}"

# Create profiles directory
mkdir -p "$PROFILES_DIR"

echo -e "${YELLOW}2. Configuration:${NC}"
echo "   Test: $TEST_NAME"
echo "   Timeout: $TIMEOUT"
echo "   Profiles: CPU=${PROFILE_CPU} MEM=${PROFILE_MEM} BLOCK=${PROFILE_BLOCK} MUTEX=${PROFILE_MUTEX} GOROUTINE=${PROFILE_GOROUTINE}"
echo "   Output: $PROFILES_DIR"
echo ""

# Build test flags
TEST_FLAGS="-v -timeout $TIMEOUT -run $TEST_NAME"
PROFILE_FLAGS=""

if [ "$PROFILE_CPU" = true ]; then
    PROFILE_FLAGS="$PROFILE_FLAGS -cpuprofile=$PROFILES_DIR/cpu.prof"
fi

if [ "$PROFILE_MEM" = true ]; then
    PROFILE_FLAGS="$PROFILE_FLAGS -memprofile=$PROFILES_DIR/mem.prof"
fi

if [ "$PROFILE_BLOCK" = true ]; then
    PROFILE_FLAGS="$PROFILE_FLAGS -blockprofile=$PROFILES_DIR/block.prof"
fi

if [ "$PROFILE_MUTEX" = true ]; then
    PROFILE_FLAGS="$PROFILE_FLAGS -mutexprofile=$PROFILES_DIR/mutex.prof"
fi

# Run the test with profiling
echo -e "${YELLOW}3. Running test with profiling...${NC}"
echo -e "${BLUE}Command: go test $TEST_FLAGS $PROFILE_FLAGS ./tests/load/${NC}"
echo ""

if go test $TEST_FLAGS $PROFILE_FLAGS ./tests/load/ 2>&1 | tee "$PROFILES_DIR/test-output.log"; then
    echo ""
    echo -e "${GREEN}✅ Test completed successfully${NC}"
    TEST_SUCCESS=true
else
    echo ""
    echo -e "${RED}⚠️  Test completed with issues${NC}"
    TEST_SUCCESS=false
fi

# Generate goroutine profile if requested (post-test)
if [ "$PROFILE_GOROUTINE" = true ] && [ "$TEST_SUCCESS" = true ]; then
    echo ""
    echo -e "${YELLOW}Note: Goroutine profiling requires runtime instrumentation${NC}"
    echo -e "${YELLOW}Consider using pprof HTTP endpoint for live goroutine profiling${NC}"
fi

echo ""
echo -e "${BLUE}==================================================================${NC}"
echo -e "${BLUE}Profiling Complete${NC}"
echo -e "${BLUE}==================================================================${NC}"
echo ""
echo -e "${GREEN}Profiles generated in: $PROFILES_DIR${NC}"
echo ""

# List generated profiles
echo "Generated files:"
ls -lh "$PROFILES_DIR"
echo ""

# Auto-analyze if requested
if [ "$AUTO_ANALYZE" = true ]; then
    echo -e "${YELLOW}4. Analyzing profiles...${NC}"
    echo ""

    if [ "$PROFILE_CPU" = true ] && [ -f "$PROFILES_DIR/cpu.prof" ]; then
        echo -e "${BLUE}=== CPU Profile Top Functions ===${NC}"
        go tool pprof -top -cum "$PROFILES_DIR/cpu.prof" 2>/dev/null | head -20 || echo "CPU profile empty or unavailable"
        echo ""
    fi

    if [ "$PROFILE_MEM" = true ] && [ -f "$PROFILES_DIR/mem.prof" ]; then
        echo -e "${BLUE}=== Memory Profile Top Allocations ===${NC}"
        go tool pprof -top -alloc_space "$PROFILES_DIR/mem.prof" 2>/dev/null | head -20 || echo "Memory profile empty or unavailable"
        echo ""
    fi

    if [ "$PROFILE_BLOCK" = true ] && [ -f "$PROFILES_DIR/block.prof" ]; then
        echo -e "${BLUE}=== Block Profile Top Contention ===${NC}"
        go tool pprof -top "$PROFILES_DIR/block.prof" 2>/dev/null | head -20 || echo "Block profile empty or unavailable"
        echo ""
    fi
fi

echo -e "${BLUE}==================================================================${NC}"
echo -e "${GREEN}Next Steps:${NC}"
echo -e "${BLUE}==================================================================${NC}"
echo ""
echo "Interactive analysis:"

if [ "$PROFILE_CPU" = true ] && [ -f "$PROFILES_DIR/cpu.prof" ]; then
    echo "  go tool pprof -http=:8080 $PROFILES_DIR/cpu.prof"
fi

if [ "$PROFILE_MEM" = true ] && [ -f "$PROFILES_DIR/mem.prof" ]; then
    echo "  go tool pprof -http=:8081 -alloc_space $PROFILES_DIR/mem.prof"
fi

if [ "$PROFILE_BLOCK" = true ] && [ -f "$PROFILES_DIR/block.prof" ]; then
    echo "  go tool pprof -http=:8082 $PROFILES_DIR/block.prof"
fi

echo ""
echo "Command-line analysis:"
echo "  go tool pprof $PROFILES_DIR/cpu.prof"
echo "    > top10          # Top 10 functions by CPU"
echo "    > list funcName  # Show source for function"
echo "    > web            # Generate call graph"
echo ""

echo "Compare with baseline:"
echo "  go tool pprof -base=baseline.prof current.prof"
echo ""

echo "Generate flame graph (requires go-torch or pprof web UI):"
echo "  go tool pprof -http=:8080 $PROFILES_DIR/cpu.prof"
echo "  # Then navigate to View > Flame Graph"
echo ""

# Save profile metadata
cat > "$PROFILES_DIR/metadata.txt" <<EOF
Test: $TEST_NAME
Date: $(date)
Git Commit: $(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
Branch: $(git branch --show-current 2>/dev/null || echo "unknown")
Go Version: $(go version)
OS: $(uname -s)
Arch: $(uname -m)
Profiles: CPU=${PROFILE_CPU} MEM=${PROFILE_MEM} BLOCK=${PROFILE_BLOCK} MUTEX=${PROFILE_MUTEX}
Success: $TEST_SUCCESS
EOF

echo -e "${GREEN}Profile metadata saved to: $PROFILES_DIR/metadata.txt${NC}"
echo ""
