#!/bin/bash
set -e

echo "🧪 Aether Test Suite Runner"
echo "============================"
echo ""

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if docker-compose is available
if ! command -v docker-compose &> /dev/null; then
    echo -e "${RED}❌ docker-compose not found${NC}"
    echo "Please install docker-compose: https://docs.docker.com/compose/install/"
    exit 1
fi

# Function to check service health
check_service() {
    local service=$1
    local max_wait=30
    local count=0

    echo -n "Waiting for $service to be healthy..."
    while [ $count -lt $max_wait ]; do
        if docker-compose -f docker-compose.test.yml ps | grep "$service" | grep -q "healthy"; then
            echo -e " ${GREEN}✓${NC}"
            return 0
        fi
        echo -n "."
        sleep 1
        count=$((count + 1))
    done

    echo -e " ${RED}✗${NC}"
    echo -e "${RED}Service $service failed to become healthy${NC}"
    return 1
}

# Start services
echo -e "${BLUE}📦 Starting test infrastructure...${NC}"
docker-compose -f docker-compose.test.yml up -d

# Wait for services to be healthy
echo ""
check_service "postgres" || exit 1
check_service "etcd" || exit 1
check_service "redis" || exit 1

echo ""
echo -e "${GREEN}✅ All services are healthy!${NC}"
echo ""

# Run tests
echo -e "${BLUE}🏃 Running test suites...${NC}"
echo ""

# Track failures
FAILED=0

# Test 1: Scheduler (always works, in-memory)
echo -e "${BLUE}📋 Testing Scheduler...${NC}"
if go test -v ./internal/scheduler/... -timeout 30s; then
    echo -e "${GREEN}✅ Scheduler tests passed${NC}"
else
    echo -e "${RED}❌ Scheduler tests failed${NC}"
    FAILED=$((FAILED + 1))
fi
echo ""

# Test 2: Rate Limiter (uses miniredis, always works)
echo -e "${BLUE}⏱️  Testing Rate Limiter...${NC}"
if go test -v ./internal/ratelimit/... -timeout 30s; then
    echo -e "${GREEN}✅ Rate Limiter tests passed${NC}"
else
    echo -e "${RED}❌ Rate Limiter tests failed${NC}"
    FAILED=$((FAILED + 1))
fi
echo ""

# Test 3: Backup/Restore (requires PostgreSQL)
echo -e "${BLUE}💾 Testing Backup/Restore (with PostgreSQL)...${NC}"
if go test -v ./internal/backup/... -timeout 60s; then
    echo -e "${GREEN}✅ Backup/Restore tests passed${NC}"
else
    echo -e "${RED}❌ Backup/Restore tests failed${NC}"
    FAILED=$((FAILED + 1))
fi
echo ""

# Test 4: HA Failover (requires etcd)
echo -e "${BLUE}🔄 Testing HA Failover (with etcd)...${NC}"
if go test -v ./internal/ha/... -timeout 60s; then
    echo -e "${GREEN}✅ HA Failover tests passed${NC}"
else
    echo -e "${RED}❌ HA Failover tests failed${NC}"
    FAILED=$((FAILED + 1))
fi
echo ""

# Test 5: VM Lifecycle
echo -e "${BLUE}🖥️  Testing VM Lifecycle...${NC}"
if go test -v -short ./internal/runtime/vm/... -timeout 30s; then
    echo -e "${GREEN}✅ VM Lifecycle tests passed${NC}"
else
    echo -e "${RED}❌ VM Lifecycle tests failed${NC}"
    FAILED=$((FAILED + 1))
fi
echo ""

# Coverage report
echo -e "${BLUE}📊 Generating coverage report...${NC}"
go test -coverprofile=coverage.out \
    ./internal/scheduler/... \
    ./internal/ratelimit/... \
    ./internal/backup/... \
    ./internal/ha/... \
    ./internal/runtime/vm/... \
    -timeout 120s 2>/dev/null || true

if [ -f coverage.out ]; then
    echo ""
    echo -e "${BLUE}Coverage by package:${NC}"
    go tool cover -func=coverage.out | grep -E "^github.com/aether-runtime/aether/internal/(scheduler|ratelimit|backup|ha|runtime/vm)" | \
        awk '{printf "  %-50s %s\n", $1, $3}'

    echo ""
    TOTAL_COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}')
    echo -e "${BLUE}Total coverage: ${GREEN}${TOTAL_COVERAGE}${NC}"

    echo ""
    echo -e "${YELLOW}💡 To view detailed coverage in browser:${NC}"
    echo "   go tool cover -html=coverage.out"
fi

# Summary
echo ""
echo "============================"
if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✨ All tests passed!${NC}"
else
    echo -e "${RED}❌ $FAILED test suite(s) failed${NC}"
fi
echo ""

# Ask about cleanup
echo -e "${YELLOW}Keep services running? (y/n)${NC}"
read -r -n 1 KEEP_RUNNING
echo ""

if [[ ! $KEEP_RUNNING =~ ^[Yy]$ ]]; then
    echo -e "${BLUE}🧹 Stopping test infrastructure...${NC}"
    docker-compose -f docker-compose.test.yml down
    echo -e "${GREEN}✅ Services stopped${NC}"
else
    echo -e "${BLUE}Services still running. Stop them with:${NC}"
    echo "   docker-compose -f docker-compose.test.yml down"
fi

exit $FAILED
