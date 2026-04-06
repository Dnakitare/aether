# Test Infrastructure Guide

This guide explains how to run Aether's comprehensive test suite with full infrastructure support.

---

## Quick Start

```bash
# 1. Start test infrastructure (PostgreSQL, etcd, Redis)
docker-compose -f docker-compose.test.yml up -d

# 2. Run all tests
./test-all.sh

# 3. Stop infrastructure
docker-compose -f docker-compose.test.yml down
```

**That's it!** The script handles everything and shows detailed results.

---

## Test Infrastructure Services

### PostgreSQL 15
- **Purpose:** Backup/Restore tests
- **Port:** 5432
- **Database:** aether_test
- **Credentials:** postgres/postgres
- **Coverage Impact:** 22.4% → ~70%

### etcd v3.5
- **Purpose:** HA Failover tests (distributed consensus)
- **Ports:** 2379 (client), 2380 (peer)
- **Coverage Impact:** 1.6% → ~70%

### Redis 7
- **Purpose:** Optional (rate limiter tests use miniredis)
- **Port:** 6379
- **Note:** Available for manual testing

---

## Running Tests

### Option 1: Automated Script (Recommended)

```bash
./test-all.sh
```

**What it does:**
1. ✅ Starts all services via docker-compose
2. ✅ Waits for health checks to pass
3. ✅ Runs all test suites in order
4. ✅ Generates coverage report
5. ✅ Shows detailed results
6. ✅ Optionally stops services

**Output includes:**
- Individual test suite results
- Coverage by package
- Total coverage percentage
- Pass/fail summary

### Option 2: Manual Testing

```bash
# Start services
docker-compose -f docker-compose.test.yml up -d

# Wait for services (check health)
docker-compose -f docker-compose.test.yml ps

# Run specific test suites
go test ./internal/scheduler/...      # In-memory (always works)
go test ./internal/ratelimit/...      # miniredis (always works)
go test ./internal/backup/...         # Requires PostgreSQL
go test ./internal/ha/...             # Requires etcd
go test -short ./internal/runtime/vm/... # VM tests

# Run all with coverage
go test -cover ./internal/...

# Stop services
docker-compose -f docker-compose.test.yml down
```

### Option 3: Individual Services

Start only what you need:

```bash
# Only PostgreSQL (for backup tests)
docker-compose -f docker-compose.test.yml up -d postgres
go test ./internal/backup/...

# Only etcd (for HA tests)
docker-compose -f docker-compose.test.yml up -d etcd
go test ./internal/ha/...

# Stop specific service
docker-compose -f docker-compose.test.yml stop postgres
```

---

## Test Coverage Matrix

| Component | Without Infrastructure | With Infrastructure | Test Suite |
|-----------|------------------------|---------------------|------------|
| **Scheduler** | 95.8% ✅ | 95.8% ✅ | In-memory, always works |
| **Rate Limiter** | 86.6% ✅ | 86.6% ✅ | Uses miniredis, always works |
| **Backup/Restore** | 22.4% ⚠️ | ~70% ✅ | Requires PostgreSQL |
| **HA Failover** | 1.6% ⚠️ | ~70% ✅ | Requires etcd |
| **VM Lifecycle** | 68.8% ✅ | 68.8% ✅ | Works locally |

**Overall Coverage:**
- **Without infrastructure:** ~55%
- **With infrastructure:** ~75-80%

---

## Troubleshooting

### Services Won't Start

```bash
# Check if ports are already in use
lsof -i :5432  # PostgreSQL
lsof -i :2379  # etcd
lsof -i :6379  # Redis

# View service logs
docker-compose -f docker-compose.test.yml logs postgres
docker-compose -f docker-compose.test.yml logs etcd

# Restart services
docker-compose -f docker-compose.test.yml restart
```

### Tests Timing Out

```bash
# Increase timeout
go test ./internal/backup/... -timeout 120s
go test ./internal/ha/... -timeout 120s
```

### Connection Refused Errors

```bash
# Wait for services to be fully healthy
docker-compose -f docker-compose.test.yml ps

# All services should show "healthy" status
# If not, wait a few more seconds and try again
```

### Clean Start

```bash
# Stop everything and remove volumes
docker-compose -f docker-compose.test.yml down -v

# Start fresh
docker-compose -f docker-compose.test.yml up -d
```

---

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:15-alpine
        env:
          POSTGRES_DB: aether_test
          POSTGRES_USER: postgres
          POSTGRES_PASSWORD: postgres
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

      etcd:
        image: quay.io/coreos/etcd:v3.5.0
        ports:
          - 2379:2379
        options: >-
          --health-cmd "etcdctl endpoint health"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run tests
        run: go test -v -coverprofile=coverage.out ./internal/...

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          file: ./coverage.out
```

---

## Performance Notes

### Service Startup Time
- PostgreSQL: ~3-5 seconds
- etcd: ~2-3 seconds
- Redis: ~1-2 seconds
- **Total:** ~10 seconds max

### Test Execution Time
- Scheduler: ~2-3 seconds
- Rate Limiter: ~0.5 seconds
- Backup/Restore: ~10-15 seconds (with PostgreSQL)
- HA Failover: ~15-20 seconds (with etcd)
- VM Lifecycle: ~5-10 seconds (short mode)
- **Total:** ~35-50 seconds

### Resource Usage
- **Memory:** ~500MB total (all services)
- **Disk:** ~100MB (volumes)
- **CPU:** Minimal during tests

---

## Advanced Usage

### Custom Test Database

```yaml
# Edit docker-compose.test.yml
environment:
  POSTGRES_DB: my_custom_db
  POSTGRES_USER: myuser
  POSTGRES_PASSWORD: mypassword
```

```bash
# Update connection string in tests if needed
export POSTGRES_URL="postgres://myuser:mypassword@localhost:5432/my_custom_db"
```

### Multiple etcd Nodes (Cluster Testing)

```yaml
# Add to docker-compose.test.yml
etcd2:
  image: quay.io/coreos/etcd:v3.5.0
  command:
    - /usr/local/bin/etcd
    - --name=etcd-test-2
    - --initial-cluster=etcd-test=http://etcd:2380,etcd-test-2=http://etcd2:2380
  ports:
    - "2389:2379"
```

### Persistent Test Data

```bash
# Keep data between test runs
docker-compose -f docker-compose.test.yml down
# (without -v flag)

# Data persists in named volumes:
# - postgres_test_data
# - etcd_test_data
# - redis_test_data
```

### View Service Data

```bash
# PostgreSQL
docker-compose -f docker-compose.test.yml exec postgres \
  psql -U postgres -d aether_test -c "SELECT * FROM test_agents;"

# etcd
docker-compose -f docker-compose.test.yml exec etcd \
  etcdctl get --prefix /aether/

# Redis
docker-compose -f docker-compose.test.yml exec redis \
  redis-cli KEYS "*"
```

---

## FAQ

**Q: Do I need Docker Desktop?**
A: Yes, or Docker Engine + docker-compose CLI on Linux.

**Q: Can I run tests without Docker?**
A: Some tests (Scheduler, Rate Limiter) work without Docker. For full coverage, you'd need to install PostgreSQL and etcd natively.

**Q: Why do some tests still show low coverage in short mode?**
A: Tests skip infrastructure-dependent operations in `-short` mode to avoid failures when services aren't available. This is by design.

**Q: How do I run only fast tests?**
A: Use `-short` flag: `go test -short ./internal/...`
This skips infrastructure-dependent tests.

**Q: Can I use this for development?**
A: Yes! Keep services running during development:
```bash
docker-compose -f docker-compose.test.yml up -d
# Develop and run tests repeatedly
docker-compose -f docker-compose.test.yml down  # When done
```

**Q: What about race detection?**
A: Add `-race` flag to any test command:
```bash
go test -race ./internal/...
```

**Q: How do I generate HTML coverage report?**
A: After running tests:
```bash
go test -coverprofile=coverage.out ./internal/...
go tool cover -html=coverage.out
```

---

## Quick Reference

### Commands Cheat Sheet

```bash
# Start all services
docker-compose -f docker-compose.test.yml up -d

# Check service health
docker-compose -f docker-compose.test.yml ps

# View logs
docker-compose -f docker-compose.test.yml logs -f

# Run all tests
./test-all.sh

# Run with race detection
go test -race ./internal/...

# Run with coverage
go test -coverprofile=coverage.out ./internal/...

# View coverage
go tool cover -html=coverage.out

# Stop services
docker-compose -f docker-compose.test.yml down

# Clean everything (including volumes)
docker-compose -f docker-compose.test.yml down -v
```

---

## Summary

✅ **Easy:** One command to start infrastructure
✅ **Fast:** Services ready in ~10 seconds
✅ **Complete:** Full coverage testing (75-80%)
✅ **Clean:** Isolated from system
✅ **CI-Ready:** Same setup for local and CI

**Get started:**
```bash
docker-compose -f docker-compose.test.yml up -d && ./test-all.sh
```
