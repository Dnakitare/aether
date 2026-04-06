# Getting Started - Local Development

**Last Updated:** 2026-02-15
**Status:** Beta v0.2.0

This guide helps you set up Aether for local development and testing.

> **Note:** Aether is in pre-alpha. This guide focuses on running tests and developing components. End-to-end agent execution is not yet fully integrated.

---

## Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Development Workflow](#development-workflow)
- [Running Tests](#running-tests)
- [Project Structure](#project-structure)
- [What Works Today](#what-works-today)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

### Required

- **Go 1.24+** - [Install Go](https://go.dev/doc/install)
- **Docker & Docker Compose** - [Install Docker](https://docs.docker.com/get-docker/)
- **Git** - For cloning the repository
- **8GB RAM** minimum (16GB recommended)

### Optional

- **PostgreSQL client** (`psql`) - For database inspection
- **Redis client** (`redis-cli`) - For cache inspection
- **golangci-lint** - For code linting
- **KVM support** - Required for running Firecracker VMs (Linux only)

### System Check

```bash
# Verify Go version
go version  # Should be 1.24 or higher

# Verify Docker
docker --version
docker-compose --version

# Check Docker is running
docker ps
```

---

## Quick Start

### Step 1: Clone Repository

```bash
git clone https://github.com/dnakitare/aether.git
cd aether
```

### Step 2: Install Dependencies

```bash
# Download Go dependencies
go mod download

# Verify dependencies
go mod verify
```

### Step 3: Build

```bash
# Build the Aether binary
go build -o aether ./cmd/aether

# Verify build
./aether --version  # May not be implemented yet
```

### Step 4: Start Infrastructure (Optional)

For integration tests, start the backing services:

```bash
# Start PostgreSQL, Redis, etcd
docker-compose -f deployments/docker/docker-compose.dev.yml up -d

# Wait for services to be ready (30 seconds)
sleep 30

# Check service health
docker-compose -f deployments/docker/docker-compose.dev.yml ps
```

You should see:
- PostgreSQL on port 5433
- Redis on port 6380
- etcd on port 2379

### Step 5: Run Tests

```bash
# Unit tests (fast, no infrastructure required)
go test -short ./...

# Integration tests (requires infrastructure from Step 4)
go test ./tests/integration/...

# Specific component tests
go test -v ./internal/scheduler/...
go test -v ./internal/auth/...
go test -v ./internal/ratelimit/...
```

---

## Development Workflow

### Typical Development Cycle

```bash
# 1. Create a feature branch
git checkout -b feature/my-feature

# 2. Make code changes
vim internal/scheduler/scheduler.go

# 3. Run tests
go test -short ./internal/scheduler/...

# 4. Run linter (if installed)
golangci-lint run ./internal/scheduler/...

# 5. Build and verify
go build ./...

# 6. Run full test suite
go test ./...

# 7. Commit changes
git add .
git commit -m "Add feature X to scheduler"
```

### Hot Reload (Optional)

For faster iteration, use [air](https://github.com/cosmtrek/air):

```bash
# Install air
go install github.com/cosmtrek/air@latest

# Run with hot reload
air
```

---

## Running Tests

### Test Categories

```bash
# Unit tests (fast, no external dependencies)
go test -short ./...

# Integration tests (requires Docker infrastructure)
go test ./tests/integration/...

# Chaos tests (stress testing)
go test ./tests/chaos/...

# Specific component comprehensive tests
go test -v -run Comprehensive ./internal/scheduler/
go test -v -run Comprehensive ./internal/backup/
go test -v -run Comprehensive ./internal/ha/
```

### Test Coverage

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...

# View coverage in browser
go tool cover -html=coverage.out

# View coverage by package
go tool cover -func=coverage.out | grep total
```

### Test Filtering

```bash
# Run only tests matching a pattern
go test -run TestScheduler ./internal/scheduler/

# Run specific test
go test -run TestSchedulerBinPacking ./internal/scheduler/

# Verbose output
go test -v ./internal/scheduler/

# With race detection
go test -race ./internal/scheduler/
```

### Docker-Based Integration Tests

```bash
# Start test infrastructure
docker-compose -f docker-compose.test.yml up -d

# Wait for ready
sleep 30

# Run integration tests
go test -v ./tests/integration/...

# Stop infrastructure
docker-compose -f docker-compose.test.yml down
```

---

## Project Structure

```
aether/
├── cmd/
│   └── aether/              # Main entry point (under construction)
│       ├── main.go          # CLI main (basic)
│       └── server.go        # HTTP server (partial)
│
├── internal/                # Private application code
│   ├── api/                 # HTTP REST API (partial) 🚧
│   ├── auth/                # JWT and API key auth ✅
│   ├── backup/              # Backup/restore system ✅
│   ├── ha/                  # High availability/leader election ✅
│   ├── ratelimit/           # Rate limiting ✅
│   ├── recovery/            # Checkpoint/restore (partial) 🚧
│   ├── runtime/             # VM lifecycle management (partial) 🚧
│   ├── scheduler/           # Agent placement ✅
│   └── state/               # State store (Redis/PostgreSQL) ✅
│
├── pkg/
│   └── api/                 # Public API types
│
├── tests/
│   ├── integration/         # Integration tests
│   └── chaos/               # Chaos testing helpers
│
├── deployments/
│   ├── docker/              # Docker Compose files
│   └── terraform/           # Infrastructure as Code (partial) 🚧
│
└── docs/                    # Documentation
    ├── api/                 # API reference
    ├── architecture/        # Architecture and ADRs
    └── deployment/          # Deployment guides

Legend: ✅ Functional  🚧 Partial  ❌ Planned
```

---

## What Works Today

### ✅ Fully Functional Components

These components have comprehensive tests and are production-quality:

1. **Scheduler** (`internal/scheduler/`)
   - Bin-packing, spread, best-fit placement strategies
   - Event-driven architecture
   - 95.8% test coverage
   ```bash
   go test -v ./internal/scheduler/ -run Comprehensive
   ```

2. **Authentication** (`internal/auth/`)
   - JWT token generation and validation
   - API key management
   - RBAC (role-based access control)
   - 78% test coverage
   ```bash
   go test -v ./internal/auth/
   ```

3. **Rate Limiting** (`internal/ratelimit/`)
   - Token bucket algorithm
   - Multi-tier rate limiting
   - Redis-backed distributed rate limiting
   - 85% test coverage
   ```bash
   go test -v ./internal/ratelimit/ -run Comprehensive
   ```

4. **High Availability** (`internal/ha/`)
   - etcd-based leader election
   - State replication
   - Automatic failover
   - 71% test coverage
   ```bash
   go test -v ./internal/ha/ -run Comprehensive
   ```

5. **Backup/Restore** (`internal/backup/`)
   - PostgreSQL and Redis backup
   - Point-in-time recovery
   - Compression and verification
   - 68% test coverage
   ```bash
   go test -v ./internal/backup/ -run Comprehensive
   ```

6. **State Store** (`internal/state/`)
   - Redis-backed state management
   - Distributed locks
   - Key-value operations
   - 67% test coverage
   ```bash
   go test -v ./internal/state/
   ```

### 🚧 Partially Implemented

These components exist but need integration work:

- **VM Lifecycle** (`internal/runtime/vm/`) - Code exists, needs wiring
- **HTTP API** (`internal/api/`) - Basic structure, endpoints incomplete
- **Checkpoint/Restore** (`internal/recovery/`) - Design complete, partial impl

### ❌ Not Yet Started

- CLI commands (`./aether agent create`, etc.)
- End-to-end agent workflow
- Observability stack integration
- Kafka messaging

---

## Troubleshooting

### Problem: Tests Fail with "connection refused"

**Cause:** Infrastructure services (PostgreSQL, Redis, etcd) not running

**Solution:**
```bash
# Start services
docker-compose -f deployments/docker/docker-compose.dev.yml up -d

# Wait 30 seconds for initialization
sleep 30

# Verify services
docker-compose -f deployments/docker/docker-compose.dev.yml ps
```

### Problem: Port already in use

**Cause:** Previous containers still running or other services using ports

**Solution:**
```bash
# Stop all containers
docker-compose -f deployments/docker/docker-compose.dev.yml down

# Check for processes on ports
lsof -i :5433  # PostgreSQL
lsof -i :6380  # Redis
lsof -i :2379  # etcd

# Kill if needed
kill <PID>
```

### Problem: go.mod dependency errors

**Solution:**
```bash
# Clean module cache
go clean -modcache

# Re-download dependencies
go mod download

# Tidy dependencies
go mod tidy
```

### Problem: Tests skip with "infrastructure not available"

**Cause:** Tests detect missing Docker services and skip gracefully

**Solution:** This is expected behavior. Either:
1. Start Docker infrastructure (see above)
2. Run only unit tests: `go test -short ./...`

### Problem: Build fails on macOS with Firecracker errors

**Cause:** Firecracker requires Linux with KVM

**Solution:**
- Development on macOS: Skip VM-related tests
- Use `go build -tags nomicrovm ./...` (if tag exists)
- Or develop in Linux VM/container

### Problem: Test coverage numbers don't match docs

**Cause:** Docs may be outdated

**Solution:**
```bash
# Get actual current coverage
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep total
```

---

## Next Steps

### For New Contributors

1. **Read the architecture docs:**
   - [Architecture Overview](architecture/ARCHITECTURE.md)
   - [ADR-001: Firecracker VMs](architecture/adr/001-firecracker-vms.md)
   - [ADR-002: Distributed Scheduler](architecture/adr/002-distributed-scheduler.md)

2. **Explore the codebase:**
   - Start with `internal/scheduler/scheduler.go` (most complete)
   - Look at test files to understand usage patterns
   - Check `pkg/api/types.go` for core data structures

3. **Pick a task:**
   - See [LAUNCH_ACTION_PLAN.md](../docs/archive/LAUNCH_ACTION_PLAN.md) for roadmap
   - Good first issues: Wire API endpoints, add tests, improve docs

### For Production Deployment

**Don't.** Aether is pre-alpha and not production-ready.

Wait for:
- **Alpha release** (March 2026) - Basic end-to-end functionality
- **Beta release** (April 2026) - Production-ready features
- **v1.0 release** (Q3 2026) - Full production hardening

See [PRODUCTION_DEPLOYMENT.md](deployment/PRODUCTION_DEPLOYMENT.md) for future plans.

---

## Additional Resources

- **Architecture:** [docs/architecture/ARCHITECTURE.md](architecture/ARCHITECTURE.md)
- **API Reference:** [docs/api/API_REFERENCE.md](api/API_REFERENCE.md)
- **Contributing:** [CONTRIBUTING.md](../CONTRIBUTING.md)
- **Project Status:** [START_HERE.md](../docs/archive/START_HERE.md)
- **Launch Plan:** [LAUNCH_ACTION_PLAN.md](../docs/archive/LAUNCH_ACTION_PLAN.md)

---

## Questions?

- **Issues:** [GitHub Issues](https://github.com/dnakitare/aether/issues)
- **Discussions:** [GitHub Discussions](https://github.com/dnakitare/aether/discussions)

---

**Happy coding!** 🚀
