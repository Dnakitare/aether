# CI/CD Pipeline

## Overview

The Aether project uses GitHub Actions for continuous integration and continuous deployment. The pipeline automatically runs on every push to `main`, `beta/*`, or `develop` branches, and on pull requests to `main` or `develop`.

## Pipeline Jobs

### 1. Unit Tests (`test`)
- **Purpose**: Run unit tests with race detection and coverage analysis
- **Coverage Threshold**: 50% minimum required to pass
- **Commands**:
  ```bash
  go test -v -race -coverprofile=coverage.out -covermode=atomic ./internal/... ./pkg/...
  ```
- **Artifacts**: Coverage report uploaded to Codecov
- **Duration**: ~2-3 minutes

### 2. Code Quality (`code-quality`)
- **Purpose**: Enforce code formatting and quality standards
- **Checks**:
  - Format check with `gofmt`
  - Go vet static analysis
  - golangci-lint with 5m timeout
- **Duration**: ~1-2 minutes

### 3. Integration Tests (`integration-tests`)
- **Purpose**: Test with real PostgreSQL database
- **Infrastructure**: PostgreSQL 16 Alpine container
  - Database: `aether_test`
  - User: `aether`
  - Health checks every 10s
- **Tests Run**:
  - All integration tests (`./tests/integration/...`)
  - Checkpoint recovery tests
  - Checkpoint integrity tests
- **Environment**: `TEST_DATABASE_URL` configured automatically
- **Duration**: ~5-10 minutes

### 4. Security Scanning (`security`)
- **Purpose**: Detect security vulnerabilities and issues
- **Scanners**:
  - **gosec**: Security scanner with severity analysis
    - Fails if HIGH severity issues found
    - Reports MEDIUM and LOW issues
  - **govulncheck**: Known vulnerability detection
- **Artifacts**: Security scan results JSON uploaded
- **Duration**: ~1-2 minutes

### 5. Build (`build`)
- **Purpose**: Compile the Aether binary
- **Dependencies**: Requires `test` and `code-quality` to pass
- **Output**: Binary artifact (`bin/aether`)
- **Retention**: 7 days
- **Duration**: ~1 minute

### 6. Docker Build (`docker`)
- **Purpose**: Build and test Docker images
- **Trigger**: Only on push to `main` or `beta/*` branches
- **Dependencies**: Requires `build` and `security` to pass
- **Tags**: Branch name and commit SHA
- **Cache**: GitHub Actions cache enabled
- **Note**: Does not push images (push: false)
- **Duration**: ~3-5 minutes

### 7. Summary (`summary`)
- **Purpose**: Aggregate all job results
- **Runs**: Always, even if jobs fail
- **Output**: Table showing all job statuses
- **Failure**: Exits with error if any job failed

## Workflow Triggers

```yaml
on:
  push:
    branches: [ main, beta/*, develop ]
  pull_request:
    branches: [ main, develop ]
```

## Environment Requirements

### Local Development
- Go 1.24
- PostgreSQL (for integration tests)
- Docker (for Docker builds)

### CI Environment
- Ubuntu Latest
- Go 1.24 with module caching
- PostgreSQL 16 service container
- Docker Buildx

## Coverage Requirements

| Package | Minimum Coverage |
|---------|-----------------|
| Overall | 50% |
| internal/audit | 47.8% ✅ |
| internal/database | 31.0% ✅ |
| internal/runtime | 38.8% ✅ |
| internal/runtime/agent | 57.9% ✅ |

## Running Locally

### Unit Tests
```bash
# Run all unit tests with coverage
go test -v -race -coverprofile=coverage.out -covermode=atomic ./internal/... ./pkg/...

# Check coverage threshold
coverage=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
echo "Coverage: $coverage%"
```

### Integration Tests
```bash
# Start PostgreSQL
docker run -d --name postgres-test \
  -e POSTGRES_USER=aether \
  -e POSTGRES_PASSWORD=aether_test_pass \
  -e POSTGRES_DB=aether_test \
  -p 5432:5432 \
  postgres:16-alpine

# Set environment variable
export TEST_DATABASE_URL="postgres://aether:aether_test_pass@localhost:5432/aether_test?sslmode=disable"

# Run integration tests
go test -v -race ./tests/integration/... -timeout 15m

# Run checkpoint tests
go test -v ./tests/integration -run TestCheckpointRecovery -timeout 10m
go test -v ./tests/integration -run TestCheckpoint -timeout 10m

# Cleanup
docker stop postgres-test && docker rm postgres-test
```

### Code Quality
```bash
# Format check
gofmt -s -l .

# Go vet
go vet ./...

# golangci-lint
golangci-lint run --timeout=5m
```

### Security Scanning
```bash
# Install tools
go install github.com/securego/gosec/v2/cmd/gosec@latest
go install golang.org/x/vuln/cmd/govulncheck@latest

# Run gosec
gosec -fmt=json -out=gosec-report.json ./...

# Run govulncheck
govulncheck ./...
```

### Build
```bash
# Build binary
mkdir -p bin
go build -v -ldflags="-s -w" -o bin/aether ./cmd/aether

# Verify
./bin/aether --version || ./bin/aether --help
```

### Docker Build
```bash
# Build image
docker build -f deployments/docker/Dockerfile -t aether:local .

# Test image
docker run --rm aether:local --version
```

## Makefile Integration

The CI pipeline can also be run using Makefile targets:

```bash
# Run all CI checks
make ci

# Individual checks
make test          # Unit tests
make coverage      # Coverage report
make lint          # Linters
make fmt-check     # Format check
make vet           # Go vet
make build         # Build binary
make integration-test  # Integration tests
```

## Troubleshooting

### Coverage Below Threshold
- Check which packages have low coverage: `go tool cover -func=coverage.out`
- Add tests for uncovered packages
- Review `docs/TESTING_SESSION_SUMMARY.md` for testing patterns

### Integration Tests Failing
- Ensure PostgreSQL service is healthy
- Check `TEST_DATABASE_URL` environment variable
- Verify database migrations are up to date
- Check test logs for specific errors

### Security Scan Failures
- Review gosec report: `.github/workflows/security-scan-results`
- Address HIGH severity issues immediately
- Investigate MEDIUM/LOW issues for false positives
- Update code to fix legitimate security concerns

### Docker Build Failures
- Check Dockerfile syntax
- Verify all required files are present
- Review build context and .dockerignore
- Check for missing dependencies

## Pipeline Duration

| Stage | Typical Duration |
|-------|-----------------|
| Setup | 30s |
| Unit Tests | 2-3 min |
| Code Quality | 1-2 min |
| Integration Tests | 5-10 min |
| Security Scan | 1-2 min |
| Build | 1 min |
| Docker Build | 3-5 min (if triggered) |
| **Total** | **10-15 min** (without Docker) |
| **Total** | **13-20 min** (with Docker) |

## Best Practices

1. **Run Tests Locally First**: Use `make ci` before pushing
2. **Keep Tests Fast**: Unit tests should complete in seconds
3. **Mock External Dependencies**: Use mocks for unit tests
4. **Fix Failures Immediately**: Don't let CI stay red
5. **Monitor Coverage**: Aim to maintain or increase coverage
6. **Review Security Scan Results**: Address security issues promptly
7. **Keep CI Configuration Updated**: Update Go version and actions regularly

## Related Documentation

- [Testing Session Summary](TESTING_SESSION_SUMMARY.md) - Testing patterns and practices
- [Database Testing Summary](DATABASE_TESTING_SUMMARY.md) - Database testing details
- [Makefile](../Makefile) - Local development commands
- [CI Workflow](.github/workflows/ci.yml) - Complete workflow definition
