# Aether Developer Diary

**A solo developer's journey building an AI agent runtime from scratch — with an AI pair programmer.**

*Project: Aether — "Docker for AI agents"*
*Timeline: January 26 – April 5, 2026*
*120 commits, 48,000 lines of Go, one developer, one AI assistant*

---

## The Idea

Aether started with a simple question: what would it look like to run untrusted AI agent workloads with the same isolation guarantees that cloud providers give VMs, but with the developer experience of Docker?

The answer was Firecracker microVMs — the same technology AWS Lambda uses — wrapped in a multi-tenant runtime with intelligent scheduling, state persistence, and observability. A platform where you POST an agent config and get back a hardware-isolated execution environment in milliseconds.

The scope was ambitious for a solo developer. But I wasn't working alone.

---

## Phase 1: Foundation (January 26, 2026)

**The first commit** landed on January 26th. By the end of that same day, Phase 1 was complete: a working runtime that could create, start, stop, and destroy Firecracker microVMs.

The core design decision was to make the runtime a thin orchestration layer. The `Runtime` struct holds an in-memory map of agents, a `vm.Manager` for Firecracker lifecycle, and an optional `StateStore` interface for persistence:

```go
type Runtime struct {
    vmManager   *vm.Manager
    stateStore  StateStore  // nil = in-memory only
    agents      map[api.AgentID]*agent.Agent
    mu          sync.RWMutex
}
```

This interface-based design paid dividends later. When we added PostgreSQL persistence, nothing in the runtime package changed — we just passed a `*PostgresStore` that satisfied the `StateStore` interface.

**Lesson learned:** Design for interfaces early. The cost is near zero and the flexibility is enormous.

---

## Phase 2-3: Orchestration and Security (January 31)

Five days later, both phases landed in a single day. The scheduler with three placement strategies (bin-packing, spread, best-fit), the HTTP REST API, JWT authentication, RBAC, and tenant isolation.

The scheduler was designed as a pluggable `Placer` that takes an `AgentRequest` and a slice of `*Node` and returns the best placement:

```go
type Placer struct {
    strategy PlacementStrategy
}

func (p *Placer) SelectNode(req *AgentRequest, nodes []*Node) (*Node, error) {
    switch p.strategy {
    case BinPacking:  // maximize utilization
    case Spread:      // distribute evenly
    case BestFit:     // minimize waste
    }
}
```

Anti-affinity was implemented as a constraint rather than a strategy — agents from the same tenant are spread across nodes to limit blast radius. This distinction matters: strategies determine *how* to score nodes, constraints determine *which* nodes are eligible.

The API followed standard patterns: gorilla/mux router, middleware chain (request tracking → request ID → tracing → logging → recovery → rate limiting → CORS), and handler functions that extract tenant context from JWT claims.

**Pitfall #1: The premature "production-ready" claim.** After Phase 6 completed on February 1st, the README declared Aether "production-ready." It wasn't. Not even close. The API handlers existed but weren't wired to the router. The PostgreSQL store had method signatures but no SQL. The VM lifecycle code was designed but never tested end-to-end. This would come back to bite us.

---

## Phase 4-6: The Speed Run (February 1)

Phases 4 through 6 — observability, advanced features, and production hardening — all landed on the same day. This is where AI-assisted development showed both its power and its danger.

**What was built:** OpenTelemetry tracing with Jaeger export, Prometheus metrics with custom collectors, Grafana dashboards, Kafka-based distributed scheduling queue, etcd leader election, Redis-backed rate limiting, HashiCorp Vault integration, backup/restore, Terraform modules for AWS/GCP/Azure, Helm charts, and Kubernetes manifests.

**What was actually working:** The individual components compiled and had unit tests. But the integration was incomplete. The API server's `Start()` method didn't actually listen. The PostgreSQL queries returned "not implemented." The Firecracker VM config was generated but never passed to a real process.

**The critical deadlock.** During the code review on February 1st, we found a deadlock in the VM pre-warming pool. The pool's `Acquire()` method held a mutex while calling into the VM manager, which tried to acquire the same lock through a callback. Classic lock ordering violation. Fixed with a two-phase approach: check availability under lock, then create outside the lock.

```
d9b3005 Fix critical deadlock in prewarming pool
```

**Lesson learned:** AI can generate architecturally sound code at incredible speed, but integration is where bugs live. The pieces were well-designed in isolation. The connections between them were where things broke.

---

## The Reality Check (February 15)

Two weeks of silence, then a pivotal day.

```
c1eeff1 Update README to reflect pre-alpha status and reality
9a7fd8d Add critical documentation fixes for pre-alpha status
```

This was the day we got honest. The README was rewritten from "production-ready" to "Alpha v0.1.0 — not ready for production use." Every false claim was either removed or corrected. The coverage numbers were adjusted from aspirational to measured.

Then the real work began — making the claims true.

```
a1765d2 Add PostgreSQL persistence layer for agent CRUD operations
f4143c6 Wire HTTP API server with PostgreSQL persistence for Alpha
1283fb8 Complete Firecracker VM configuration with proper JSON marshaling
cde62ce Add end-to-end integration tests for Alpha
```

Four commits that turned a well-designed skeleton into a working system. The PostgreSQL store got real SQL with parameterized queries. The API server's `setupRoutes()` was wired to actual handlers. The Firecracker config generator produced valid JSON that the hypervisor could parse.

**Lesson learned:** Honesty about project status is not a setback — it's the prerequisite for progress. The moment we stopped claiming features that didn't work, we could focus on making them work.

---

## Beta Development (February 15-24)

With alpha shipped, beta development moved in structured weekly sprints:

**Week 1:** Distributed tracing. OpenTelemetry spans were added to the scheduler and runtime. Trace IDs were injected into structured logs automatically, so you could correlate a log line to its distributed trace.

**Week 2:** Prometheus metrics and Grafana dashboards. Custom metric families for API requests, agent operations, scheduler decisions, and resource usage. Four pre-built Grafana dashboards.

**Week 3:** Load testing framework. Performance benchmarks for scheduler placement and agent creation. The in-memory queue fallback was implemented here — if Kafka is unavailable, the distributed scheduler degrades to a local in-memory queue rather than failing.

**Week 4:** Checkpoint/restore for agent state. Metadata-level checkpointing was implemented (full VM snapshot via CRIU deferred to v1.0). API improvements: bulk create/delete endpoints with concurrency control.

**Week 5:** Infrastructure. Terraform VPC module, Kubernetes manifests with proper security contexts, Docker multi-stage build with distroless base image.

**Week 6:** Documentation sweep, release notes, upgrade guide. Beta v0.2.0 tagged.

---

## The CI/CD Gauntlet (February 15-24)

Ten commits tell the story of getting CI to actually work:

```
69b06c2 Fix gosec hang in ci.yml and Go version mismatch in Dockerfile
48886b3 Fix remaining CI pipeline failures
95911ce Fix security scanning CI pipeline failures
85225e1 Fix deploy pipeline blocked by Trivy image scan findings
2a74d5e Fix Cosign image signing failure due to uppercase registry path
4384c62 Fix deploy pipeline: Trivy auth, single image ref, skip deploy without AWS
7e1ae6e Fix workflow parse error: remove secrets context from job-level if conditions
b1e6b16 Fix deploy pipeline: add continue-on-error to migrations job
```

Every one of these was a GitHub Actions lesson learned the hard way:

- **gosec hangs** on large codebases if you don't set a timeout
- **Trivy** blocks the pipeline on medium-severity CVEs in base images that you can't fix
- **Cosign** image signing fails if the registry path has uppercase characters
- **GitHub Actions** `if:` conditions can't reference `secrets` context at the job level
- **Database migrations** in CI need `continue-on-error` because the test database might not have the schema yet

**Lesson learned:** CI/CD is its own engineering discipline. Budget real time for it. "Just add a workflow file" is never just that.

---

## The Module Rename (April 1-4)

A project-wide module rename from an earlier anonymous path to `github.com/dnakitare/aether`. Every import in 148 Go files had to change. Five commits touched nearly every file in the codebase:

```
5e06af8 chore: rename module path to github.com/dnakitare/aether
99d855b chore: update internal packages for module rename
7768196 test: update integration, chaos, load, and security tests
627da1c chore(ha): update HA tests for module rename
273d45a chore: update Makefile targets and gitignore patterns
```

This was also when critical runtime bugs were found and fixed:

```
e5800b4 Fix nil panic, data corruption, hot loop, and unbounded reads
e869a72 fix(api): resolve critical bugs in log streaming, rate limiting, and server lifecycle
```

The nil panic was in the agent log streaming path — if the agent didn't have a log file, the handler would dereference nil. The hot loop was in the rate limiter's Redis polling — a missing `time.Sleep` in the retry path consumed 100% CPU. The unbounded read was in `parseJSON` — no request body size limit, allowing a malicious client to OOM the server.

**Lesson learned:** Module renames are a good time for a fresh-eyes review. The mechanical find-and-replace forces you to read every file, and that's when you spot bugs you'd otherwise miss.

---

## The Polish Pass (April 4-5)

This is where the project went from "works" to "shippable." A thorough review exposed issues at every layer:

**Repository hygiene:** A 39MB compiled binary, 30MB test binary, and 2MB of profiling data were committed to git. 27 development-artifact markdown files cluttered the root directory. A duplicate `terraform/` directory existed alongside `deployments/terraform/`. All cleaned up.

**Code correctness:**
- `CreateAgent` in PostgreSQL wasn't transactional — if the agent INSERT failed after the tenant UPSERT succeeded, orphaned tenant records accumulated. Wrapped in `BeginTx`/`Commit`/`defer Rollback`.
- JWT accepted any-length secret keys for HS256. Added 32-character minimum enforcement.
- Bulk create/delete endpoints didn't record Prometheus metrics or scheduler allocations, making bulk-created agents invisible to monitoring. Fixed.
- The recovery middleware could panic on double `WriteHeader` if the original handler had already started writing the response. Added a `wroteHeader` guard.
- Scaler policy endpoints accepted unvalidated path parameters. Added name validation.

**Documentation accuracy:** The README claimed Kafka was "Not Yet Implemented" when it was fully integrated with DLQ and fallback. Observability was listed as "Partial" despite having 13 files of OpenTelemetry tracing. The project structure section listed 9 packages when 23 existed. The Go version said 1.21 when the project required 1.24. The status said Alpha when Beta was released two months prior.

**Deployment:** Docker Compose volume mounts pointed to `deployments/monitoring/grafana/` but the actual path was `deployments/grafana/provisioning/`. Helm chart dependencies used `12.x.x` version syntax which isn't valid SemVer. AlertManager had a hardcoded `localhost:9095` webhook that didn't exist. The UPGRADE_GUIDE referenced three nonexistent file paths.

Every single one of these was found by reading the actual code and verifying claims against reality. Not by running a linter or a test suite — by a human (assisted by AI) opening files and asking "is this true?"

---

## What We Built

As of April 5, 2026, Aether is 48,000 lines of Go across 148 files, with 404 test functions and 14 benchmarks.

**The runtime** manages Firecracker microVM lifecycles with create, start, stop, destroy, and checkpoint operations. State is persisted to PostgreSQL with transactional writes. Agents are tracked in-memory with a read-write mutex and reconciled from the database on restart.

**The API** serves 20+ REST endpoints through a gorilla/mux router with a 7-layer middleware chain: request tracking, request ID injection, distributed tracing, structured logging, panic recovery, rate limiting, and CORS. Authentication uses JWT (HS256 or RS256) with per-tenant RBAC and API key support.

**The scheduler** supports three placement strategies and anti-affinity constraints. In distributed mode, it uses etcd for leader election, Kafka for the scheduling queue (with automatic in-memory fallback), and Redis for node state tracking.

**Deployment** is covered by a distroless Docker image (non-root, 39MB), Kubernetes manifests with Kustomize, a Helm chart with PostgreSQL/Redis subchart dependencies, and Terraform modules for AWS.

**Observability** includes OpenTelemetry distributed tracing, Prometheus metrics with 12 custom metric families, Grafana dashboards, AlertManager integration, and structured JSON logging with trace ID correlation.

---

## What We Learned

### 1. AI accelerates architecture, humans ensure integration

AI is extraordinary at generating well-structured, idiomatically correct Go code for individual components. The scheduler, the auth system, the rate limiter — each was architecturally sound on its own. But the wiring between components — API handler → runtime → state store → database — that's where every real bug lived. The deadlock, the nil panics, the missing metrics, the double-WriteHeader crash: all integration bugs.

### 2. Documentation lies until verified

The README spent two months claiming features that didn't work. Coverage numbers were aspirational. File paths in guides pointed to directories that didn't exist. The only way to fix this was to read every claim and verify it against the code. Not the tests, not the CI output — the actual source files.

### 3. CI/CD is an engineering project, not a configuration task

Eight commits to fix the deployment pipeline. Each one a distinct failure mode: security scanners hanging, image signing failing on case sensitivity, workflow syntax errors that only manifest at runtime. Budget real time for CI/CD and expect iteration.

### 4. Honest status reporting enables progress

The most productive day in the project was February 15th — when we admitted the project was pre-alpha, not production-ready. That admission unlocked the focus to actually implement the PostgreSQL queries, wire the API routes, and write the E2E tests. You can't fix what you won't acknowledge is broken.

### 5. The last 10% is 50% of the work

Going from "all tests pass" to "ready to ship" required: removing committed binaries from git history, moving 27 files out of the root directory, fixing 15+ documentation inaccuracies, adding transactional safety to database writes, enforcing JWT key length, adding nil guards to middleware, wiring metrics into bulk endpoints, fixing volume mount paths in Docker Compose, correcting Helm chart version syntax, and updating Go version references in 5 documentation files.

None of these showed up as test failures.

---

## By the Numbers

| Metric | Value |
|--------|-------|
| Calendar time | 10 weeks |
| Total commits | 120 |
| Lines of Go | 48,069 |
| Test functions | 404 |
| Internal packages | 23 |
| REST API endpoints | 20+ |
| Database migrations | 3 |
| Architecture Decision Records | 8 |
| CI/CD fix commits | 10 |
| Critical bugs found in review | 6 |
| Documentation inaccuracies fixed | 15+ |
| Files moved out of root directory | 27 |

---

## What's Next

Aether Beta v0.2.0 is shippable. All core components are integrated, tested, and documented. The deployment infrastructure works. The documentation matches reality.

For v1.0, three things remain:
- **Full VM checkpoint/restore** via CRIU (currently metadata-only)
- **Multi-region support** for geographic redundancy
- **80%+ test coverage** (currently ~35%)

The architecture supports all three. The foundation is solid. The hard part — making it real — is done.

---

*Built by a solo developer with an AI pair programmer. Every line reviewed. Every claim verified.*
