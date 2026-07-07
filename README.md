# Aether

**AI Agent Runtime with Hardware-Level Isolation** (Beta v0.2.0)

[![Build Status](https://img.shields.io/github/actions/workflow/status/dnakitare/aether/ci.yml?branch=main)](https://github.com/dnakitare/aether/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/go-1.24-blue)](https://golang.org/dl/)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)
[![Development Status](https://img.shields.io/badge/status-beta-blue)](https://github.com/dnakitare/aether)

Aether runs each AI agent inside its own [Firecracker](https://firecracker-microvm.github.io/) microVM, the same KVM technology behind AWS Lambda. You POST an agent config to a REST API and get back a hardware-isolated execution environment with multi-tenant quotas, JWT/RBAC auth, an in-process scheduler, PostgreSQL-backed state, and OpenTelemetry observability.

Think "Docker for AI agents," but with VM-grade isolation for untrusted workloads.

> **Project status: Beta v0.2.0.** Aether is a single-region control plane: HTTP API, in-process scheduler, PostgreSQL persistence, Redis-backed rate limiting, and OpenTelemetry observability, deployable to a single cloud region via Docker, Kubernetes, Helm, or Terraform. It is not recommended for production workloads yet. See [docs/V1_SCOPE.md](docs/V1_SCOPE.md) for the v1.0 scope and shipping criteria.

---

## What it does

### Isolation and multi-tenancy
- **Hardware isolation**: each agent runs in a Firecracker microVM (KVM), not a shared-kernel container
- **Multi-tenant**: per-tenant quotas (CPU, memory, disk) and tenant isolation on every API path
- **Auth**: JWT plus API keys, with role-based access control (platform admin, tenant admin, developer, viewer)

### Orchestration
- **Scheduler**: bin-packing, spread, and best-fit placement strategies, with anti-affinity constraints
- **Resource quotas**: per-tenant limits enforced atomically at creation time
- **Rate limiting**: token-bucket limiter (Redis-backed, degrades to allow on infrastructure errors)

### State and observability
- **Durable state**: agent lifecycle persisted to PostgreSQL with transactional writes
- **Checkpoints**: metadata-level agent-state checkpoint and restore (full VM snapshot via CRIU is future work)
- **Observability**: OpenTelemetry tracing, Prometheus metrics, structured JSON logs with trace correlation

---

## Use cases

- **AI agent platforms**: run LLM agents and autonomous systems with untrusted-code isolation
- **Sandboxed code execution**: REPLs, notebooks, evaluation harnesses
- **CI/CD runners**: isolated build environments
- **Multi-tenant SaaS**: any workload that needs strong per-tenant isolation

---

## Architecture

Aether is a single-instance control plane in front of Firecracker compute:

```
                    ┌──────────────────────┐
   HTTP clients ──▶ │      API Server      │  JWT/RBAC, rate limiting,
                    │     (gorilla/mux)     │  tenant quotas, handlers
                    └──────────┬───────────┘
                               │
                    ┌──────────▼───────────┐
                    │   In-process         │  bin-packing / spread /
                    │   Scheduler          │  best-fit + anti-affinity
                    └──────────┬───────────┘
                               │ placement
                    ┌──────────▼───────────┐
                    │   Runtime + VM       │  create / start / stop /
                    │   Manager            │  destroy Firecracker VMs
                    └──────────┬───────────┘
                               │
              ┌────────────────┴────────────────┐
         ┌────▼─────┐                      ┌─────▼─────┐
         │PostgreSQL│  durable state       │   Redis   │  rate limiting
         │          │  + audit log         │           │
         └──────────┘                      └───────────┘
```

**Components**:
- **API server**: REST API, middleware chain (request tracking, request ID, tracing, logging, recovery, rate limiting, CORS), JWT/RBAC auth
- **Scheduler**: in-process placement with three strategies and anti-affinity
- **Runtime + VM manager**: Firecracker microVM lifecycle (Linux/KVM; macOS builds skip VM operations for development)
- **PostgreSQL**: durable agent state and the audit log
- **Redis**: token-bucket rate limiting

---

## Quick start

### Prerequisites
- **OS**: Linux with KVM for VM operations, or macOS for development without VMs
- **Go**: 1.24 or later
- **Docker**: for PostgreSQL and Redis
- **Firecracker** (optional): required only for real VM operations on Linux

### Build and run (about 5 minutes)

```bash
# 1. Clone and build
git clone https://github.com/dnakitare/aether.git
cd aether
go mod download
go build -o aether ./cmd/aether

# 2. Start dependencies (PostgreSQL + Redis)
docker-compose up -d
sleep 5

# 3. Configure
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/aether?sslmode=disable"
export JWT_SECRET="change-me-to-a-random-string-min-32-chars"
export SERVER_ADDRESS=":8080"

# 4. Run migrations and start the server
./aether migrate up
./aether server
# Server listens on http://localhost:8080
```

### CLI

```bash
# Agent lifecycle
./aether agent create --name "my-agent" --image "python:3.11"
./aether agent list
./aether agent logs <agent-id>
./aether agent stop <agent-id>
./aether agent destroy <agent-id>

# State checkpoints (metadata-level)
./aether agent checkpoint create <agent-id>
./aether agent checkpoint list <agent-id>
./aether agent checkpoint restore <agent-id> --version 3

# Database migrations
./aether migrate up
./aether migrate version

# Runtime daemon without the HTTP API
./aether daemon
```

### Tests

```bash
# Unit tests, no infrastructure needed
go test -short ./...

# With the race detector (as CI runs them)
go test -short -race ./...

# Integration tests (needs Docker: PostgreSQL + Redis)
docker-compose -f docker-compose.test.yml up -d
go test ./tests/integration/...
```

---

## What works in Beta v0.2.0

Working end to end:
- Agent lifecycle (create, start, stop, destroy) with PostgreSQL persistence
- Fully wired HTTP REST API with the complete middleware chain
- Firecracker VM lifecycle and configuration
- JWT auth, API keys, and RBAC with multi-tenant isolation
- In-process scheduler with three placement strategies and anti-affinity
- Per-tenant resource quotas, enforced atomically
- Redis-backed token-bucket rate limiting
- OpenTelemetry tracing, Prometheus metrics, structured logging
- Metadata-level agent-state checkpoint and restore

Known limitations:
- Firecracker needs Linux with KVM; macOS development skips VM operations
- Checkpoint/restore saves metadata only; full VM snapshot via CRIU is planned
- Auto-scaling policies exist but the evaluation loop is not production-tested
- Single region, single instance; multi-region and multi-instance are not in scope for v1.0

---

## Documentation

- [Architecture overview](docs/architecture/ARCHITECTURE.md)
- [Architecture Decision Records](docs/architecture/adr/)
- [Security model](SECURITY.md)
- [v1.0 scope and shipping criteria](docs/V1_SCOPE.md)
- [Upgrade guide](UPGRADE_GUIDE.md)

---

## Project structure

```
aether/
├── cmd/aether/          # CLI: server, daemon, agent, migrate
├── internal/
│   ├── api/             # HTTP REST API, handlers, middleware, health
│   ├── audit/           # HMAC-chained audit log
│   ├── auth/            # JWT, API keys, RBAC
│   ├── cli/             # terminal output helpers
│   ├── config/          # Viper-based configuration
│   ├── database/        # migration runner (golang-migrate)
│   ├── observability/   # OpenTelemetry tracing, Prometheus metrics
│   ├── optimization/    # VM pre-warming pool
│   ├── ratelimit/       # token-bucket rate limiting (Redis)
│   ├── recovery/        # agent-state checkpointing
│   ├── runtime/         # agent + Firecracker VM lifecycle
│   ├── scaler/          # policy-based auto-scaling
│   ├── scheduler/       # placement strategies + in-process queue
│   ├── state/           # PostgreSQL + Redis persistence
│   ├── shutdown/        # graceful shutdown coordination
│   └── tenant/          # multi-tenant quota management
├── pkg/api/             # public API types and interfaces
├── deployments/         # Docker, Kubernetes, Terraform (AWS + GCP), Prometheus, Grafana
├── helm/aether/         # Helm chart (PostgreSQL + Redis deps)
├── migrations/          # embedded SQL migrations
├── docs/                # architecture, ADRs, API reference, guides
└── tests/
    ├── integration/     # end-to-end + component integration
    ├── security/        # auth, injection, tenant isolation
    └── chaos/           # fault-injection helpers
```

---

## Testing and coverage

- **349 test functions** across unit, integration, security, and chaos suites
- **~53% overall coverage** (unit tests, short mode). Well-covered core: config (89%), rate limiting (85%), auth (82%), scheduler (85%). Lower on the runtime and API handlers, which is the focus for v1.0.
- CI runs build, `go vet`, `golangci-lint`, and `go test -race` on every push

The v1.0 target is ≥ 70% coverage on the supported surface. See [docs/V1_SCOPE.md](docs/V1_SCOPE.md).

---

## Roadmap

### Beta v0.2.0 (current)
Single-region control plane integrated and tested: HTTP API, in-process scheduler, PostgreSQL state, Redis rate limiting, OpenTelemetry observability, metadata checkpointing, and single-cloud deployment.

### v1.0 (target: Q3 2026)
Harden the single-region surface to a defensible release. Shipping criteria, each measurable:

- [ ] ≥ 70% test coverage on the supported surface
- [ ] One external security review (auth, multi-tenant isolation, Firecracker boundary), P0/P1 findings closed
- [ ] 1,000-agent load test on a single region, raw numbers published in `docs/V1_LOAD_TEST.md`
- [ ] One external adopter running a v1.0 release candidate on a real workload for at least two weeks

### Deferred
- Full CRIU-based VM checkpoint/restore → v1.1 (metadata-level lands in v1.0)
- Multi-region → not scheduled

If the criteria aren't met by Q3 2026, the date slips. A v1.0 that means something beats a v1.0 that ships on time.

---

## Security

Aether layers its defenses:

1. **VM isolation**: Firecracker hardware virtualization for untrusted agent code
2. **Multi-tenancy**: tenant checks on every API path; platform-level operations gated behind a distinct platform-admin role
3. **Auth**: JWT (HS256/RS256, minimum 32-char secret enforced) plus API keys with RBAC
4. **Application**: input validation, upload magic-byte checks, request-body size limits
5. **Audit**: HMAC-chained audit log

To report a vulnerability, see [SECURITY.md](SECURITY.md).

---

## Contributing

Contributions are welcome. This is a beta maintained by a solo developer.

1. Fork the repository
2. Create a feature branch
3. Write tests for new behavior
4. Run `go test -short -race ./...` and `golangci-lint run` before opening a PR

---

## License

Apache License 2.0. See [LICENSE](LICENSE). Apache 2.0 gives patent protection and is friendly to commercial use.

---

## Acknowledgments

- [Firecracker](https://firecracker-microvm.github.io/) for the microVM foundation
- [PostgreSQL](https://www.postgresql.org/) for durable state
- [Redis](https://redis.io/) for rate limiting
- [OpenTelemetry](https://opentelemetry.io/) for observability

---

## Project stats

- **Language**: Go 1.24
- **Lines of code**: ~35,000 including tests
- **Test functions**: 349
- **Overall coverage**: ~53% (short mode); v1.0 target ≥ 70% on the supported surface
- **Direct dependencies**: 21 (see `go.mod`)
- **Status**: Beta v0.2.0, single-region. Not yet recommended for production.
