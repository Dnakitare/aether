# Integration Tests

This directory contains integration tests that verify data integrity and transaction boundaries with real database and Redis instances.

## Prerequisites

### PostgreSQL
You need a PostgreSQL database for checkpoint transaction tests:

```bash
# Using Docker
docker run -d \
  --name aether-test-postgres \
  -e POSTGRES_PASSWORD=test \
  -e POSTGRES_DB=aether_test \
  -p 5433:5432 \
  postgres:15-alpine

# Wait for startup
sleep 5

# Run migrations
export DATABASE_URL="postgres://postgres:test@localhost:5433/aether_test?sslmode=disable"
go run cmd/aether/main.go migrate up
```

### Redis
You need a Redis instance for state transaction tests:

```bash
# Using Docker
docker run -d \
  --name aether-test-redis \
  -p 6380:6379 \
  redis:7-alpine
```

## Running Tests

### All Integration Tests
```bash
export TEST_DATABASE_URL="postgres://postgres:test@localhost:5433/aether_test?sslmode=disable"
export TEST_REDIS_URL="redis://localhost:6380/1"

go test -v ./tests/integration/
```

### Checkpoint Tests Only
```bash
export TEST_DATABASE_URL="postgres://postgres:test@localhost:5433/aether_test?sslmode=disable"

go test -v ./tests/integration/ -run TestCheckpoint
```

### State Tests Only
```bash
export TEST_REDIS_URL="redis://localhost:6380/1"

go test -v ./tests/integration/ -run TestSave -run TestDelete -run TestConcurrent
```

### With Race Detection
```bash
go test -v -race ./tests/integration/
```

## Test Coverage

### Checkpoint Integrity Tests (`checkpoint_integrity_test.go`)

| Test | Purpose | Verifies |
|------|---------|----------|
| TestCheckpointTransactionAtomicity | Checkpoint creation and cleanup are atomic | Both insert and retention cleanup happen in same transaction |
| TestCheckpointConcurrentCreation | No duplicate versions under load | Serializable isolation prevents race conditions |
| TestCheckpointTransactionRollback | Transaction rollback on error | Failed checkpoint doesn't leave partial state |
| TestCheckpointRetentionCleanup | Old checkpoints cleaned up atomically | Retention policy enforced within transaction |
| TestCheckpointConcurrentReadWrite | Reads don't block writes | MVCC allows concurrent access |
| TestCheckpointMultipleAgents | Agent isolation | Each agent has independent version sequence |

**Critical Test:** `TestCheckpointConcurrentCreation` creates 50 checkpoints from 10 concurrent goroutines and verifies:
- No duplicate version numbers
- All versions are sequential (1 to 50)
- No missing versions

This test directly validates the fix for the race condition described in the remediation plan.

### State Integrity Tests (`state_integrity_test.go`)

| Test | Purpose | Verifies |
|------|---------|----------|
| TestSaveAgentStateAtomicity | Agent state and tenant set updated together | Redis pipeline ensures atomicity |
| TestDeleteAgentStateAtomicity | Agent state and tenant set deleted together | Redis pipeline ensures atomicity |
| TestConcurrentAgentStateUpdates | Concurrent saves are atomic | No duplicate entries in tenant set |
| TestConcurrentAgentStateMixedOperations | Mixed save/delete operations | Consistency maintained under load |
| TestMultipleTenantIsolation | Tenant isolation | No cross-tenant contamination |
| TestAgentStateConsistencyAfterFailure | Consistency after updates | State and tenant set stay in sync |

**Critical Test:** `TestConcurrentAgentStateMixedOperations` performs 20 concurrent updates and 20 concurrent deletes, then verifies:
- Tenant set has exactly the expected count
- Only updated agents exist
- All deleted agents are gone
- No orphaned entries in tenant set

## Expected Results

All tests should pass with:
- ✅ No race conditions detected with `-race` flag
- ✅ No duplicate versions in checkpoint tests
- ✅ No duplicate agents in tenant set tests
- ✅ Consistent state after all concurrent operations

## Troubleshooting

### Tests Skip with "DATABASE_URL not set"
Set the `TEST_DATABASE_URL` environment variable to your test database connection string.

### Tests Skip with "REDIS_URL not set"
Set the `TEST_REDIS_URL` environment variable to your test Redis instance.

### Connection Refused Errors
Ensure PostgreSQL and Redis are running on the specified ports:
```bash
# Check PostgreSQL
psql "postgres://postgres:test@localhost:5433/aether_test?sslmode=disable" -c "SELECT version();"

# Check Redis
redis-cli -p 6380 ping
```

### Migration Errors
Ensure migrations have been applied to the test database:
```bash
export DATABASE_URL="postgres://postgres:test@localhost:5433/aether_test?sslmode=disable"
go run cmd/aether/main.go migrate up
```

### Race Detector Warnings
If `-race` flag shows warnings, this indicates actual race conditions that need fixing. Do not ignore these.

## CI/CD Integration

Add to your CI pipeline:

```yaml
# .github/workflows/integration-tests.yml
name: Integration Tests

on: [push, pull_request]

jobs:
  integration-tests:
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:15-alpine
        env:
          POSTGRES_PASSWORD: test
          POSTGRES_DB: aether_test
        ports:
          - 5433:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

      redis:
        image: redis:7-alpine
        ports:
          - 6380:6379
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run migrations
        env:
          DATABASE_URL: postgres://postgres:test@localhost:5433/aether_test?sslmode=disable
        run: go run cmd/aether/main.go migrate up

      - name: Run integration tests
        env:
          TEST_DATABASE_URL: postgres://postgres:test@localhost:5433/aether_test?sslmode=disable
          TEST_REDIS_URL: redis://localhost:6380/1
        run: go test -v -race ./tests/integration/
```

## Cleanup

After testing, remove test containers:

```bash
docker rm -f aether-test-postgres aether-test-redis
```
