# Changelog

All notable changes to Aether will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Hardening and simplification pass: fixed confirmed concurrency and multi-tenant
isolation bugs, and cut the codebase to a single-region core.

### Fixed (robustness)
- Scheduler node listing is serialized through a snapshot before JSON encoding; marshaling live node pointers raced the scheduler's map writes (a process-crashing Go fatal on `GET /scheduler/nodes`)
- `Agent.GetInfo` no longer mutates shared state under a read lock
- Removed a recursive read-lock deadlock in the VM pre-warming pool's refill path
- The scheduling queue index is rebuilt after enqueue so `Remove` can't evict the wrong agent
- Padded short agent IDs before slicing the TAP device name (was a panic for IDs under 8 characters)
- Discard a claimed pre-warmed VM when cold-create fails, preventing a pool leak

### Fixed (security / multi-tenancy)
- Introduced a platform-admin role distinct from tenant admin. Quota writes, cross-tenant reads, cluster topology, and global scaler policies now require platform authority; a tenant key can no longer read or mutate another tenant's state or raise its own quota
- Token role is now derived correctly from scopes; `quota:write`/`scheduler:write` no longer imply global admin, and an unrecognized scope no longer yields a permission-less role
- Rate limiting keys off the authenticated tenant from validated claims and ignores the spoofable `X-Tenant-ID`/`X-User-ID` headers; the limiter now fails open on infrastructure errors instead of returning 500 for every request
- `requirePermission` denies by default when auth is enabled and claims are absent (was fail-open)
- Internal error details are logged server-side and no longer returned to clients
- CORS wildcard sends a literal `*` instead of reflecting an arbitrary `Origin`

### Removed
- The multi-instance distributed scheduler (etcd shard manager, Kafka queue, node registry), HA/leader election, and backup/restore subsystems. Aether is single-region and single-instance.
- Unwired code: the HashiCorp Vault client, an orphaned Kafka wrapper, an unused AWS S3 client, and several dead packages (retry, routing, monitoring, redis-quota store, dead health-check helpers)
- Dependencies dropped: HashiCorp Vault, segmentio/kafka-go, go.etcd.io/etcd, the AWS SDK, and the duplicate `go-redis/redis/v8`. Direct dependencies went from ~130 to 21.
- Net effect: roughly 14,000 lines removed; 23 internal packages down to 16.

## [0.2.0-beta] - 2026-04-06

Beta: the single-region control plane, integrated and tested end to end.

### Added
- Fully wired HTTP REST API with a complete middleware chain (request tracking, request ID, tracing, logging, recovery, rate limiting, CORS)
- PostgreSQL state store with transactional agent CRUD
- In-process scheduler with bin-packing, spread, and best-fit strategies plus anti-affinity constraints
- JWT and API-key authentication with RBAC and multi-tenant isolation
- Per-tenant resource quotas, enforced atomically
- Redis-backed token-bucket rate limiting
- OpenTelemetry tracing, Prometheus metrics, and Grafana dashboards
- Metadata-level agent-state checkpoint and restore
- Database migrations CLI (`migrate up`/`down`/`version`)
- Deployment: Docker, Kubernetes with Kustomize, a Helm chart, and Terraform modules

### Fixed
- Made `CreateAgent` transactional to avoid orphaned tenant rows
- Enforced a 32-character minimum on HS256 JWT secrets
- Added a `wroteHeader` guard to the recovery middleware
- Removed committed binaries from the repository and corrected documentation to match the code

## [0.1.0] - 2026-02-15

Alpha: minimal end-to-end agent lifecycle.

### Added
- Firecracker microVM integration for hardware-level isolation
- Agent lifecycle (create, start, stop, destroy)
- HTTP REST API wired to the scheduler and PostgreSQL persistence
- JWT authentication with RBAC
- CLI for agent management
- End-to-end integration tests

[Unreleased]: https://github.com/dnakitare/aether/compare/v0.2.0-beta...HEAD
[0.2.0-beta]: https://github.com/dnakitare/aether/releases/tag/v0.2.0-beta
[0.1.0]: https://github.com/dnakitare/aether/releases/tag/v0.1.0
