# Aether

**Modern AI Agent Runtime with Hardware-Level Isolation** (Alpha v0.1.0)

[![Build Status](https://img.shields.io/github/workflow/status/dnakitare/aether/CI)](https://github.com/dnakitare/aether/actions)
[![Go Version](https://img.shields.io/badge/go-1.21-blue)](https://golang.org/dl/)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)
[![Development Status](https://img.shields.io/badge/status-alpha-yellow)](https://github.com/dnakitare/aether)

Aether is a runtime for AI agents with secure isolation, intelligent orchestration, and observability. Built on **Firecracker microVMs**, Aether is designed to run untrusted workloads safely and efficiently.

**Think Docker for AI agents** – but with security and multi-tenancy from day one.

> ⚠️ **Project Status: Alpha v0.1.0**
> Aether has reached alpha with end-to-end agent lifecycle working. Core components are integrated and functional. Not ready for production use. Expected beta release: April 2026.

---

## ✨ Vision

### 🔒 Security First

- **Hardware-Level Isolation**: Firecracker microVMs with KVM virtualization
- **Multi-Tenant Architecture**: Complete tenant isolation (network, compute, data)
- **Secrets Management**: Designed for HashiCorp Vault integration
- **Authentication**: JWT + API keys with RBAC

### 🚀 Production Goals

- **High Availability**: Multi-AZ deployment with automatic failover
- **Disaster Recovery**: Automated backups, point-in-time recovery
- **Observability**: Distributed tracing (Jaeger), metrics (Prometheus), logs
- **Scalability**: Designed for 10,000+ concurrent agents

### 🧠 Intelligent Orchestration

- **Smart Scheduling**: Bin-packing, spread, and best-fit placement strategies
- **Auto-Scaling**: Policy-based horizontal scaling (planned)
- **Resource Quotas**: Per-tenant CPU, memory, disk limits
- **Rate Limiting**: Token bucket algorithm with multi-tier support

---

## 🎯 Use Cases

- **AI Agent Platforms**: Run LLM agents, autonomous systems, AI assistants
- **Code Execution Services**: Sandboxed code execution (e.g., Jupyter, REPL)
- **CI/CD Runners**: Isolated build environments
- **Function-as-a-Service**: Serverless function runtime
- **Multi-Tenant SaaS**: Any workload requiring strong isolation

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Load Balancer                           │
│                   (TLS, WAF, DDoS Protection)                │
└────────────────────────┬────────────────────────────────────┘
                         │
            ┌────────────┼────────────┐
            │            │            │
   ┌────────▼──────┐  ┌──▼──────┐  ┌▼──────────┐
   │  API Server 1  │  │API Srv 2│  │API Srv 3  │
   │  (Stateless)   │  │(Stless) │  │(Stateless)│
   └────────┬───────┘  └───┬─────┘  └─────┬─────┘
            │              │              │
            └──────────────┼──────────────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
         ┌────▼────┐  ┌────▼────┐  ┌───▼─────┐
         │Scheduler│  │Scheduler│  │Scheduler│
         │(Leader) │──│(Follower)──│(Follower)│
         └────┬────┘  └─────────┘  └─────────┘
              │
              │ Placement Decisions
              │
    ┌─────────▼──────────────────────────────┐
    │      Compute Nodes (10-50+ nodes)      │
    │  ┌──────────┐  ┌──────────┐           │
    │  │  Node 1  │  │  Node 2  │  ...      │
    │  │┌────┐┌───┐│ │┌────┐┌───┐│          │
    │  ││VM1 ││VM2││ ││VM3 ││VM4││          │
    │  │└────┘└───┘│ │└────┘└───┘│          │
    │  └──────────┘  └──────────┘           │
    └────────────────────────────────────────┘
                      │
      ┌───────────────┼───────────────┐
      │               │               │
 ┌────▼────┐    ┌────▼────┐    ┌────▼────┐
 │PostgreSQL│    │  Redis  │    │  etcd   │
 │(Multi-AZ)│    │(Multi-AZ)│    │(Cluster)│
 └─────────┘    └─────────┘    └─────────┘
```

**Key Components**:
- **API Servers**: HTTP REST API (functional, fully wired) ✅
- **Schedulers**: Distributed scheduler with leader election (functional) ✅
- **Compute Nodes**: Firecracker VM management (functional, integrated) ✅
- **PostgreSQL**: Durable state, audit logs (functional with state store) ✅
- **Redis**: Cache, distributed locks, rate limiting (integrated) ✅
- **etcd**: Leader election, distributed coordination (integrated) ✅

---

## 🚀 Quick Start

### Prerequisites

- **OS**: Linux with KVM support (or macOS for development without VMs)
- **Go**: 1.21 or later
- **Docker**: For dependencies (PostgreSQL, Redis, etcd)
- **Firecracker** (optional): For full VM functionality on Linux

### Alpha Quick Start (5 minutes)

```bash
# 1. Clone and build
git clone https://github.com/dnakitare/aether.git
cd aether
go mod download
go build -o aether ./cmd/aether

# 2. Start infrastructure
docker-compose -f deployments/docker/docker-compose.dev.yml up -d

# Wait for PostgreSQL to be ready
sleep 5

# 3. Set environment variables
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/aether?sslmode=disable"
export JWT_SECRET="your-secret-key-change-in-production"
export SERVER_ADDRESS=":8080"

# 4. Start the Aether server
./aether server

# Server will start on http://localhost:8080
# Logs will show: "Aether server started successfully"
```

### Running Your First Agent (CLI)

```bash
# In another terminal
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/aether?sslmode=disable"

# Create and start an agent
./aether agent create --name "my-first-agent" --image "python:3.11"

# List agents
./aether agent list

# View agent logs
./aether agent logs <agent-id>

# Stop agent
./aether agent stop <agent-id>

# Clean up
./aether agent destroy <agent-id>
```

### Running Tests

```bash
# Unit tests (fast, no infrastructure required)
go test -short ./...

# Integration tests (requires Docker infrastructure)
docker-compose -f docker-compose.test.yml up -d
go test ./tests/integration/...

# E2E tests (validates complete workflow)
go test -v ./tests/integration/e2e_workflow_test.go

# Comprehensive test suites
go test -v ./internal/scheduler/... -run Comprehensive
go test -v ./internal/backup/... -run Comprehensive
go test -v ./internal/ha/... -run Comprehensive
```

### What Works in Alpha v0.1.0

✅ **Core Functionality** (End-to-End Working):
- **Agent Lifecycle**: Create, start, stop, destroy agents with PostgreSQL persistence
- **HTTP API Server**: Fully wired REST API with all components integrated
- **Firecracker VM Management**: Complete VM lifecycle with proper configuration
- **JWT Authentication**: Token generation, validation, and API key management
- **Distributed Scheduler**: Bin-packing, spread, and anti-affinity placement strategies
- **PostgreSQL State Store**: Durable agent state with CRUD operations
- **Redis Integration**: Caching, distributed locks, rate limiting
- **HA Leader Election**: etcd-based consensus for multi-instance deployments
- **Rate Limiting**: Token bucket algorithm with multi-tier support
- **Backup/Restore**: Automated PostgreSQL + Redis backup and recovery
- **Security**: Input validation, injection prevention, RBAC, tenant isolation

✅ **Testing**:
- **E2E Integration Tests**: Complete agent lifecycle validation
- **Comprehensive Test Suites**: Scheduler, HA, backup, auth, rate limiting
- **Infrastructure-Aware**: Tests skip gracefully when dependencies unavailable
- **CI-Ready**: Short mode for fast CI runs, full mode for local testing

🚧 **Alpha Limitations**:
- Firecracker requires Linux with KVM (development on macOS skips VM operations)
- CLI tool has basic commands but limited functionality
- Observability stack designed but not fully implemented
- Checkpoint/restore system partially implemented
- Auto-scaling not yet implemented

❌ **Not Yet Implemented**:
- Kafka messaging integration
- Advanced observability (distributed tracing, detailed metrics)
- Production deployment automation
- Multi-region support
- Auto-scaling policies

### Running Tests

```bash
# Unit tests (fast)
go test -short ./...

# Integration tests (requires Docker infrastructure)
docker-compose -f docker-compose.test.yml up -d
go test ./tests/integration/...

# Comprehensive test suites
go test -v ./internal/scheduler/... -run Comprehensive
go test -v ./internal/backup/... -run Comprehensive
go test -v ./internal/ha/... -run Comprehensive
```

---

## 📚 Documentation

### Architecture & Design

- [Architecture Overview](docs/architecture/ARCHITECTURE.md) - System design and components
- [Architecture Decision Records (ADRs)](docs/architecture/adr/) - Design rationale

**Key ADRs**:
- [ADR-001: Firecracker for VM Isolation](docs/architecture/adr/001-firecracker-vms.md)
- [ADR-002: Distributed Scheduler](docs/architecture/adr/002-distributed-scheduler.md)
- [ADR-003: PostgreSQL + Redis State Management](docs/architecture/adr/003-state-management.md)
- [ADR-004: JWT Authentication](docs/architecture/adr/004-jwt-authentication.md)

### Development

- **Comprehensive Assessment**: See [START_HERE.md](START_HERE.md) for current project state
- **Launch Planning**: See [LAUNCH_ACTION_PLAN.md](LAUNCH_ACTION_PLAN.md) for roadmap
- **Security**: See [SECURITY.md](SECURITY.md) for security architecture

---

## 🛠️ Development

### Building from Source

```bash
# Clone and build
git clone https://github.com/dnakitare/aether.git
cd aether
go build -o aether ./cmd/aether

# Run tests with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Project Structure

```
aether/
├── cmd/
│   └── aether/              # CLI entry point (basic structure)
├── internal/
│   ├── api/                 # HTTP REST API (partial)
│   ├── runtime/             # VM lifecycle management (designed)
│   ├── scheduler/           # Agent placement (functional) ✅
│   ├── ha/                  # High availability (functional) ✅
│   ├── recovery/            # Checkpointing (partial)
│   ├── ratelimit/           # Rate limiting (functional) ✅
│   ├── state/               # State persistence (functional) ✅
│   ├── backup/              # Backup/restore (functional) ✅
│   └── auth/                # Authentication (functional) ✅
├── pkg/
│   └── api/                 # Public API types
├── deployments/
│   ├── docker/              # Docker Compose for dev/test
│   └── terraform/           # Infrastructure as Code (designed)
├── docs/                    # Architecture documentation
└── tests/
    ├── integration/         # Integration tests
    └── chaos/               # Chaos testing helpers
```

---

## 📊 Current Status

### Alpha v0.1.0 Released (February 2026)

| Component | Status | Coverage | Notes |
|-----------|--------|----------|-------|
| **Core Runtime** | ✅ Complete | 65% | Full agent lifecycle integrated |
| **HTTP API Server** | ✅ Complete | 58% | All components wired |
| **Scheduler** | ✅ Complete | 82% | Bin-packing, spread, anti-affinity |
| **VM Lifecycle** | ✅ Complete | 60% | Firecracker integrated |
| **PostgreSQL State** | ✅ Complete | 72% | Full CRUD operations |
| **HA/Leader Election** | ✅ Complete | 71% | etcd-based consensus |
| **Auth (JWT/API Key)** | ✅ Complete | 78% | RBAC, token management |
| **Rate Limiting** | ✅ Complete | 85% | Token bucket algorithm |
| **Backup/Restore** | ✅ Complete | 68% | PostgreSQL + Redis backup |
| **E2E Tests** | ✅ Complete | 75% | Full lifecycle validation |
| Checkpointing | 🟡 Partial | 40% | Design complete, impl partial |
| Observability | 🟡 Partial | 25% | Basic logging, tracing planned |
| CLI Tool | 🟡 Basic | 30% | Core commands functional |

**Overall Test Coverage**: ~35% (measured), targeting 60% for beta

### Alpha Completion Summary

- ✅ **Phase 1**: Security (auth, isolation, validation) - Complete
- ✅ **Phase 4**: High Availability - Complete
- ✅ **Phase 5**: Disaster Recovery - Complete
- ✅ **Phase 6**: Observability (design) - Complete
- ✅ **Phase 7**: Test Coverage & Integration - Complete
- ✅ **Alpha Integration**: All core components wired and functional

---

## 🗺️ Roadmap

### ✅ Alpha v0.1.0 (Released: February 2026)

**Focus**: Minimal end-to-end agent lifecycle

- [x] Wire API server to scheduler
- [x] Complete VM lifecycle integration
- [x] Basic CLI commands
- [x] End-to-end tests (create, run, destroy agent)
- [x] Developer documentation
- [x] PostgreSQL state persistence
- [x] Firecracker VM management

**Status**: ✅ Complete (February 15, 2026)

### Beta Release (Target: April 2026)

**Focus**: Production-ready features

- [ ] Observability stack (tracing, metrics, dashboards)
- [ ] Checkpoint/restore system
- [ ] Resource quotas and limits
- [ ] Multi-tenant testing
- [ ] Load testing (1,000+ agents)
- [ ] Deployment automation (Terraform)

**Timeline**: 6 weeks from alpha

### Production v1.0 (Target: Q3 2026)

- [ ] Multi-region support
- [ ] Auto-scaling
- [ ] Advanced scheduling (affinity rules, taints/tolerations)
- [ ] Kafka event streaming
- [ ] 80%+ test coverage
- [ ] Security audit
- [ ] Performance benchmarks

---

## 🔐 Security

Aether implements **defense in depth**:

1. **Application**: Input validation, injection prevention, RBAC
2. **Authentication**: JWT with short expiry, API key rotation
3. **Multi-Tenancy**: Tenant isolation in all queries
4. **Network**: TLS 1.3, VPC isolation (in production design)
5. **VM Isolation**: Firecracker hardware virtualization
6. **Infrastructure**: Encrypted at rest/transit, secrets management

**Current State**: Security foundations complete (auth, validation, isolation design). Production hardening planned for beta.

---

## 🤝 Contributing

Contributions are welcome! This project is currently in pre-alpha and maintained by a solo developer.

### How to Contribute

1. **Fork** the repository
2. **Create** a feature branch (`git checkout -b feature/amazing-feature`)
3. **Commit** your changes
4. **Push** to the branch (`git push origin feature/amazing-feature`)
5. **Open** a Pull Request

### Development Guidelines

- Follow Go best practices (`go vet`, `golangci-lint`)
- Write tests for new features (aim for 60%+ coverage)
- Update documentation for user-facing changes
- Run `go test -short ./...` before submitting PR

### Priority Areas for Contributors

- 🔴 **High Priority**: End-to-end integration, API endpoint implementation
- 🟡 **Medium Priority**: CLI tool, observability integration
- 🟢 **Low Priority**: Documentation improvements, test coverage

---

## 📜 License

Aether is licensed under the **Apache License 2.0**.

This means you can:
- ✅ Use it commercially
- ✅ Modify it
- ✅ Distribute it
- ✅ Use it privately

You must:
- 📄 Include the license and copyright notice
- 📄 State significant changes made to the code

See [LICENSE](LICENSE) for the full license text.

**Why Apache 2.0?** Patent protection, enterprise-friendly, compatible with commercial use.

---

## 🙏 Acknowledgments

- [Firecracker](https://firecracker-microvm.github.io/) - The microVM foundation
- [etcd](https://etcd.io/) - Distributed consensus
- [PostgreSQL](https://www.postgresql.org/) - Reliable data persistence
- [Redis](https://redis.io/) - Fast caching and coordination
- [OpenTelemetry](https://opentelemetry.io/) - Observability standards

---

## 📞 Support

- **Documentation**: [docs/](docs/)
- **Issues**: [GitHub Issues](https://github.com/dnakitare/aether/issues)
- **Questions**: Open a discussion on GitHub

---

## 📈 Project Stats

- **Language**: Go 1.21
- **Lines of Code**: ~30,000
- **Test Coverage**: ~35% (targeting 60% for beta)
- **Dependencies**: 30+ (see `go.mod`)
- **Development Status**: Alpha v0.1.0 (Core complete, production-ready features in progress)
- **Current Release**: v0.1.0-alpha (February 15, 2026)
- **Next Release**: Beta v0.2.0 (Target: April 2026)

---

<div align="center">

**Built for the AI agent ecosystem** 🚀

[Start Here](START_HERE.md) • [Architecture](docs/architecture/ARCHITECTURE.md) • [Launch Plan](LAUNCH_ACTION_PLAN.md)

**✨ Alpha v0.1.0 Released - Core agent lifecycle functional!**

**⚠️ Not production-ready. Beta coming April 2026.**

</div>
