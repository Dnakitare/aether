# Aether

**A runtime environment for running AI agent workloads in a secure, scalable way.**

## What We're Building

Aether is the substrate upon which AI agents execute. Think of it as a container runtime, but purpose-built for AI agent workloads.

### Core Capabilities

- **Secure Isolation** - Agents run sandboxed, unable to affect each other or escape to the host
- **Elastic Scaling** - Scale from one agent to thousands based on demand
- **Full Observability** - Every agent action logged, traced, and auditable
- **Resource Control** - CPU, memory, network, and API rate limits per agent
- **Simple API** - Deploy, monitor, and manage agents with clean interfaces

## Tech Stack

- **Language**: Go
- **Why Go**: Strong concurrency primitives, excellent tooling, proven in infrastructure (Docker, Kubernetes, etc.)

## Architecture Overview

```
┌────────────────────────────────────────────────┐
│                   Aether                        │
├────────────────────────────────────────────────┤
│  ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │  Agent   │ │  Agent   │ │  Agent   │  ...  │
│  │ Sandbox  │ │ Sandbox  │ │ Sandbox  │       │
│  └──────────┘ └──────────┘ └──────────┘       │
├────────────────────────────────────────────────┤
│  Scheduler │ Scaler │ Router │ Monitor         │
├────────────────────────────────────────────────┤
│  AuthN │ AuthZ │ Secrets │ Audit               │
├────────────────────────────────────────────────┤
│  Storage │ Network │ Queue │ Metrics           │
└────────────────────────────────────────────────┘
```

## Design Principles

1. **Security First** - Default deny, explicit allow, audit everything
2. **Simplicity** - Clear abstractions over clever optimizations
3. **Correctness** - A bug in the runtime affects all agents
4. **Operability** - Easy to deploy, monitor, debug, and upgrade

## Open Decisions

- [ ] Isolation tech: gVisor vs Firecracker vs Wasm (Wasmtime/WasmEdge)
- [ ] Orchestration: build on K8s or standalone?
- [ ] Agent communication model
- [ ] Storage backend for state
- [ ] Networking approach for agents

## Go Conventions

- Follow standard Go project layout
- Use `context.Context` for cancellation and timeouts
- Prefer stdlib where reasonable
- Use structured logging (slog)
- Table-driven tests
- Run `go fmt`, `go vet`, `golangci-lint`

## Git Commits

- Conventional commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`
- NO AI attribution in commits
- Reference issues where applicable
