# Aether API Reference

**Version:** 1.0.0
**Base URL:** `https://api.aether.example.com/v1`
**Protocol:** HTTPS only
**Authentication:** JWT Bearer tokens or API keys

---

## Table of Contents

- [Authentication](#authentication)
- [Agents](#agents)
- [Tenants](#tenants)
- [Quotas](#quotas)
- [Health & Status](#health--status)
- [Error Handling](#error-handling)
- [Rate Limiting](#rate-limiting)
- [Pagination](#pagination)
- [Webhooks](#webhooks)

---

## Authentication

Aether supports two authentication methods:

### 1. JWT Bearer Tokens

Used for user authentication with role-based access control (RBAC).

**Request:**
```http
POST /v1/auth/login
Content-Type: application/json

{
  "username": "user@example.com",
  "password": "your-password"
}
```

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2026-02-10T10:00:00Z",
  "tenant_id": "tenant-123",
  "user_id": "user-456",
  "role": "admin"
}
```

**Using the token:**
```http
GET /v1/agents
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

**Token expiration:** Tokens expire after 1 hour by default. Use the `/v1/auth/refresh` endpoint to obtain a new token.

### 2. API Keys

Used for service-to-service authentication with scoped permissions.

**Creating an API key:**
```http
POST /v1/auth/keys
Authorization: Bearer <your-jwt-token>
Content-Type: application/json

{
  "name": "Production Service",
  "scopes": ["agent:read", "agent:write"],
  "expires_in": "720h"
}
```

**Response:**
```json
{
  "key": "aether_sk_abc123def456...",
  "key_id": "key-789",
  "name": "Production Service",
  "scopes": ["agent:read", "agent:write"],
  "created_at": "2026-02-09T10:00:00Z",
  "expires_at": "2026-03-09T10:00:00Z"
}
```

**Using the API key:**
```http
GET /v1/agents
Authorization: Bearer aether_sk_abc123def456...
```

**Security best practices:**
- Store API keys securely (use environment variables or secrets management)
- Rotate keys regularly (every 30-90 days)
- Use minimum required scopes
- Revoke keys immediately when compromised

### Available Scopes

| Scope | Description |
|-------|-------------|
| `agent:read` | Read agent information |
| `agent:write` | Create and update agents |
| `agent:delete` | Delete agents |
| `agent:exec` | Execute commands in agents |
| `tenant:read` | Read tenant information |
| `tenant:write` | Update tenant settings |
| `quota:read` | View quotas and usage |
| `quota:write` | Update quota limits |

### Roles

| Role | Permissions | Use Case |
|------|-------------|----------|
| `admin` | All permissions | System administrators |
| `developer` | agent:*, quota:read | Application developers |
| `viewer` | *:read | Read-only access |
| `operator` | agent:read, agent:write, quota:read | Operations team |

---

## Agents

Agents are isolated runtime environments for AI workloads.

### Create Agent

Creates a new agent with specified resources and configuration.

**Endpoint:** `POST /v1/agents`

**Request:**
```http
POST /v1/agents
Authorization: Bearer <token>
Content-Type: application/json

{
  "name": "my-agent",
  "image": "docker.io/library/python:3.11-slim",
  "resources": {
    "cpu_count": 2,
    "memory_mb": 4096,
    "disk_mb": 20480
  },
  "env": {
    "ENVIRONMENT": "production",
    "LOG_LEVEL": "info"
  },
  "secrets": ["api-key", "db-password"],
  "labels": {
    "team": "ml-platform",
    "project": "recommendation-engine"
  }
}
```

**Response:** `201 Created`
```json
{
  "id": "agent-abc123",
  "tenant_id": "tenant-123",
  "name": "my-agent",
  "image": "docker.io/library/python:3.11-slim",
  "status": "pending",
  "resources": {
    "cpu_count": 2,
    "memory_mb": 4096,
    "disk_mb": 20480
  },
  "env": {
    "ENVIRONMENT": "production",
    "LOG_LEVEL": "info"
  },
  "secrets": ["api-key", "db-password"],
  "labels": {
    "team": "ml-platform",
    "project": "recommendation-engine"
  },
  "created_at": "2026-02-09T10:00:00Z"
}
```

**Status codes:**
- `201 Created` - Agent created successfully
- `400 Bad Request` - Invalid request body
- `401 Unauthorized` - Missing or invalid authentication
- `403 Forbidden` - Insufficient permissions
- `409 Conflict` - Agent with same name already exists
- `429 Too Many Requests` - Rate limit exceeded
- `507 Insufficient Storage` - Quota exceeded

**Validation rules:**
- `name`: 1-64 alphanumeric characters, hyphens, underscores
- `image`: Must be from allowed registry (configure via policy)
- `cpu_count`: 1-64 cores
- `memory_mb`: 128-131072 MB (128 MB to 128 GB)
- `disk_mb`: 1024-1048576 MB (1 GB to 1 TB)

**Example with curl:**
```bash
curl -X POST https://api.aether.example.com/v1/agents \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-agent",
    "image": "python:3.11-slim",
    "resources": {
      "cpu_count": 2,
      "memory_mb": 4096,
      "disk_mb": 20480
    }
  }'
```

### List Agents

Lists all agents for your tenant with optional filtering.

**Endpoint:** `GET /v1/agents`

**Query parameters:**
| Parameter | Type | Description | Default |
|-----------|------|-------------|---------|
| `status` | string | Filter by status (pending, running, stopped, failed) | - |
| `label` | string | Filter by label (e.g., `team=ml-platform`) | - |
| `page` | integer | Page number for pagination | 1 |
| `per_page` | integer | Items per page (max 100) | 20 |

**Request:**
```http
GET /v1/agents?status=running&label=team=ml-platform&page=1&per_page=20
Authorization: Bearer <token>
```

**Response:** `200 OK`
```json
{
  "agents": [
    {
      "id": "agent-abc123",
      "tenant_id": "tenant-123",
      "name": "my-agent",
      "image": "python:3.11-slim",
      "status": "running",
      "resources": {
        "cpu_count": 2,
        "memory_mb": 4096,
        "disk_mb": 20480
      },
      "labels": {
        "team": "ml-platform",
        "project": "recommendation-engine"
      },
      "created_at": "2026-02-09T10:00:00Z",
      "started_at": "2026-02-09T10:01:30Z"
    }
  ],
  "pagination": {
    "page": 1,
    "per_page": 20,
    "total": 1,
    "total_pages": 1
  }
}
```

### Get Agent

Retrieves detailed information about a specific agent.

**Endpoint:** `GET /v1/agents/{agent_id}`

**Request:**
```http
GET /v1/agents/agent-abc123
Authorization: Bearer <token>
```

**Response:** `200 OK`
```json
{
  "id": "agent-abc123",
  "tenant_id": "tenant-123",
  "name": "my-agent",
  "image": "python:3.11-slim",
  "status": "running",
  "resources": {
    "cpu_count": 2,
    "memory_mb": 4096,
    "disk_mb": 20480
  },
  "env": {
    "ENVIRONMENT": "production",
    "LOG_LEVEL": "info"
  },
  "labels": {
    "team": "ml-platform",
    "project": "recommendation-engine"
  },
  "created_at": "2026-02-09T10:00:00Z",
  "started_at": "2026-02-09T10:01:30Z",
  "metrics": {
    "cpu_usage_percent": 45.2,
    "memory_usage_mb": 2048,
    "disk_usage_mb": 10240,
    "network_rx_bytes": 1048576,
    "network_tx_bytes": 524288
  }
}
```

**Status codes:**
- `200 OK` - Agent found
- `404 Not Found` - Agent doesn't exist or belongs to different tenant

### Update Agent

Updates agent configuration (limited to stopped agents).

**Endpoint:** `PATCH /v1/agents/{agent_id}`

**Request:**
```http
PATCH /v1/agents/agent-abc123
Authorization: Bearer <token>
Content-Type: application/json

{
  "env": {
    "LOG_LEVEL": "debug"
  },
  "labels": {
    "version": "2.0"
  }
}
```

**Response:** `200 OK`
```json
{
  "id": "agent-abc123",
  "name": "my-agent",
  "status": "stopped",
  "env": {
    "ENVIRONMENT": "production",
    "LOG_LEVEL": "debug"
  },
  "labels": {
    "team": "ml-platform",
    "project": "recommendation-engine",
    "version": "2.0"
  },
  "updated_at": "2026-02-09T10:30:00Z"
}
```

**Note:** Resource limits cannot be changed after creation. Create a new agent if you need different resources.

### Start Agent

Starts a stopped agent.

**Endpoint:** `POST /v1/agents/{agent_id}/start`

**Request:**
```http
POST /v1/agents/agent-abc123/start
Authorization: Bearer <token>
```

**Response:** `202 Accepted`
```json
{
  "id": "agent-abc123",
  "status": "starting",
  "message": "Agent start initiated"
}
```

**Status transitions:**
- `stopped` → `starting` → `running`
- Typical start time: 1-3 seconds

### Stop Agent

Stops a running agent gracefully (with 30s timeout).

**Endpoint:** `POST /v1/agents/{agent_id}/stop`

**Request:**
```http
POST /v1/agents/agent-abc123/stop
Authorization: Bearer <token>
Content-Type: application/json

{
  "force": false,
  "timeout": 30
}
```

**Response:** `202 Accepted`
```json
{
  "id": "agent-abc123",
  "status": "stopping",
  "message": "Agent stop initiated"
}
```

**Parameters:**
- `force` (boolean): If true, kills agent immediately without graceful shutdown
- `timeout` (integer): Seconds to wait before force kill (default: 30)

### Delete Agent

Permanently deletes an agent and all its data.

**Endpoint:** `DELETE /v1/agents/{agent_id}`

**Request:**
```http
DELETE /v1/agents/agent-abc123
Authorization: Bearer <token>
```

**Response:** `204 No Content`

**Warning:** This action is irreversible. All agent data, logs, and checkpoints will be deleted.

### Get Agent Logs

Retrieves or streams agent logs.

**Endpoint:** `GET /v1/agents/{agent_id}/logs`

**Query parameters:**
| Parameter | Type | Description | Default |
|-----------|------|-------------|---------|
| `follow` | boolean | Stream logs in real-time | false |
| `tail` | integer | Number of lines from end | 100 |
| `since` | string | RFC3339 timestamp | - |

**Request (static logs):**
```http
GET /v1/agents/agent-abc123/logs?tail=100
Authorization: Bearer <token>
```

**Response:** `200 OK`
```
2026-02-09T10:01:35Z [INFO] Application starting
2026-02-09T10:01:36Z [INFO] Database connection established
2026-02-09T10:01:37Z [INFO] Server listening on port 8080
```

**Request (streaming logs):**
```http
GET /v1/agents/agent-abc123/logs?follow=true
Authorization: Bearer <token>
Accept: text/event-stream
```

**Response:** `200 OK` (Server-Sent Events)
```
data: 2026-02-09T10:01:35Z [INFO] Application starting

data: 2026-02-09T10:01:36Z [INFO] Processing request

data: 2026-02-09T10:01:37Z [INFO] Request completed
```

### Execute Command

Executes a command inside a running agent.

**Endpoint:** `POST /v1/agents/{agent_id}/exec`

**Request:**
```http
POST /v1/agents/agent-abc123/exec
Authorization: Bearer <token>
Content-Type: application/json

{
  "command": ["python", "-c", "print('Hello from agent')"],
  "timeout": 30
}
```

**Response:** `200 OK`
```json
{
  "exit_code": 0,
  "stdout": "Hello from agent\n",
  "stderr": "",
  "duration_ms": 125
}
```

**Security notes:**
- Requires `agent:exec` permission
- Commands run with agent's user privileges
- Timeout enforced (max 300 seconds)
- Audit logged

### Get Agent Health

Checks agent health status.

**Endpoint:** `GET /v1/agents/{agent_id}/health`

**Request:**
```http
GET /v1/agents/agent-abc123/health
Authorization: Bearer <token>
```

**Response:** `200 OK`
```json
{
  "status": "healthy",
  "checks": {
    "process": "ok",
    "memory": "ok",
    "disk": "ok",
    "network": "ok"
  },
  "last_check": "2026-02-09T10:05:00Z"
}
```

**Health statuses:**
- `healthy` - All checks passing
- `degraded` - Some checks warning
- `unhealthy` - One or more checks failing
- `unknown` - Unable to determine health

---

## Tenants

Multi-tenancy support for organizational isolation.

### Get Tenant Info

Retrieves information about your tenant.

**Endpoint:** `GET /v1/tenants/me`

**Request:**
```http
GET /v1/tenants/me
Authorization: Bearer <token>
```

**Response:** `200 OK`
```json
{
  "id": "tenant-123",
  "name": "Acme Corp",
  "tier": "enterprise",
  "created_at": "2026-01-01T00:00:00Z",
  "settings": {
    "default_agent_timeout": 3600,
    "max_agent_lifetime": 86400
  },
  "billing": {
    "plan": "enterprise",
    "usage_current_month": {
      "agents_created": 150,
      "cpu_hours": 3600,
      "memory_gb_hours": 14400
    }
  }
}
```

### Update Tenant Settings

Updates tenant configuration.

**Endpoint:** `PATCH /v1/tenants/me`

**Request:**
```http
PATCH /v1/tenants/me
Authorization: Bearer <token>
Content-Type: application/json

{
  "settings": {
    "default_agent_timeout": 7200
  }
}
```

**Response:** `200 OK`

**Note:** Requires `tenant:write` permission.

---

## Quotas

Resource quotas and usage tracking.

### Get Quota

Retrieves current quota limits and usage.

**Endpoint:** `GET /v1/quotas`

**Request:**
```http
GET /v1/quotas
Authorization: Bearer <token>
```

**Response:** `200 OK`
```json
{
  "limits": {
    "max_agents": 100,
    "max_cpu_total": 200,
    "max_memory_total_mb": 409600,
    "max_disk_total_mb": 2097152,
    "requests_per_minute": 1000
  },
  "usage": {
    "agents": 45,
    "cpu_total": 90,
    "memory_total_mb": 184320,
    "disk_total_mb": 921600
  },
  "available": {
    "agents": 55,
    "cpu_total": 110,
    "memory_total_mb": 225280,
    "disk_total_mb": 1175552
  }
}
```

---

## Health & Status

System health and status endpoints.

### Health Check

Basic health check for load balancers.

**Endpoint:** `GET /health`

**No authentication required.**

**Request:**
```http
GET /health
```

**Response:** `200 OK`
```json
{
  "status": "healthy"
}
```

### Readiness Check

Detailed readiness check including dependencies.

**Endpoint:** `GET /readiness`

**Request:**
```http
GET /readiness
```

**Response:** `200 OK`
```json
{
  "status": "ready",
  "checks": {
    "database": "ok",
    "redis": "ok",
    "etcd": "ok",
    "scheduler": "ok"
  },
  "timestamp": "2026-02-09T10:00:00Z"
}
```

**Status codes:**
- `200 OK` - System is ready
- `503 Service Unavailable` - System is not ready (includes details)

### Metrics

Prometheus-formatted metrics.

**Endpoint:** `GET /metrics`

**Request:**
```http
GET /metrics
```

**Response:** `200 OK` (Prometheus text format)
```
# HELP aether_agents_total Total number of agents
# TYPE aether_agents_total gauge
aether_agents_total{status="running"} 45
aether_agents_total{status="stopped"} 10

# HELP aether_api_requests_total Total API requests
# TYPE aether_api_requests_total counter
aether_api_requests_total{method="GET",endpoint="/v1/agents",status="200"} 12543

# HELP aether_scheduler_placement_duration_seconds Scheduler placement duration
# TYPE aether_scheduler_placement_duration_seconds histogram
aether_scheduler_placement_duration_seconds_bucket{le="0.1"} 8234
aether_scheduler_placement_duration_seconds_bucket{le="0.5"} 9876
aether_scheduler_placement_duration_seconds_sum 1234.56
aether_scheduler_placement_duration_seconds_count 10000
```

---

## Error Handling

Aether uses standard HTTP status codes and returns errors in a consistent JSON format.

### Error Response Format

```json
{
  "error": {
    "code": "QUOTA_EXCEEDED",
    "message": "Agent creation would exceed CPU quota",
    "details": {
      "requested": 16,
      "available": 10,
      "limit": 100
    },
    "request_id": "req-abc123"
  }
}
```

### Common Error Codes

| HTTP Status | Error Code | Description |
|-------------|------------|-------------|
| 400 | `INVALID_REQUEST` | Request body or parameters invalid |
| 400 | `VALIDATION_ERROR` | Field validation failed |
| 401 | `UNAUTHORIZED` | Missing or invalid authentication |
| 403 | `FORBIDDEN` | Insufficient permissions |
| 404 | `NOT_FOUND` | Resource doesn't exist |
| 409 | `CONFLICT` | Resource already exists |
| 429 | `RATE_LIMIT_EXCEEDED` | Too many requests |
| 507 | `QUOTA_EXCEEDED` | Resource quota exceeded |
| 500 | `INTERNAL_ERROR` | Server error (contact support) |
| 503 | `SERVICE_UNAVAILABLE` | System temporarily unavailable |

### Error Handling Best Practices

**Retry logic:**
```python
import time
import requests

def create_agent_with_retry(config, max_retries=3):
    for attempt in range(max_retries):
        try:
            response = requests.post(
                "https://api.aether.example.com/v1/agents",
                json=config,
                headers={"Authorization": f"Bearer {token}"}
            )

            if response.status_code == 200:
                return response.json()

            # Retry on server errors and rate limits
            if response.status_code in [429, 500, 503]:
                retry_after = int(response.headers.get("Retry-After", 5))
                time.sleep(retry_after)
                continue

            # Don't retry client errors
            response.raise_for_status()

        except requests.exceptions.RequestException as e:
            if attempt == max_retries - 1:
                raise
            time.sleep(2 ** attempt)  # Exponential backoff
```

---

## Rate Limiting

Aether enforces rate limits based on your tenant tier.

### Rate Limit Headers

Every API response includes rate limit information:

```http
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 945
X-RateLimit-Reset: 1707476400
```

- `X-RateLimit-Limit`: Maximum requests allowed in the current window
- `X-RateLimit-Remaining`: Requests remaining in current window
- `X-RateLimit-Reset`: Unix timestamp when the window resets

### Rate Limit Tiers

| Tier | Requests/Minute | Burst |
|------|----------------|-------|
| Free | 60 | 100 |
| Pro | 600 | 1000 |
| Enterprise | 6000 | 10000 |

### Handling Rate Limits

When you exceed the rate limit:

```http
HTTP/1.1 429 Too Many Requests
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1707476460
Retry-After: 60

{
  "error": {
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "Rate limit exceeded. Please retry after 60 seconds.",
    "request_id": "req-xyz789"
  }
}
```

**Best practices:**
- Monitor the `X-RateLimit-Remaining` header
- Implement exponential backoff
- Cache responses when possible
- Use webhooks instead of polling
- Contact support to upgrade tier if needed

---

## Pagination

List endpoints support cursor-based pagination.

### Request

```http
GET /v1/agents?page=2&per_page=50
Authorization: Bearer <token>
```

### Response

```json
{
  "agents": [...],
  "pagination": {
    "page": 2,
    "per_page": 50,
    "total": 150,
    "total_pages": 3,
    "next": "https://api.aether.example.com/v1/agents?page=3&per_page=50",
    "prev": "https://api.aether.example.com/v1/agents?page=1&per_page=50"
  }
}
```

**Limits:**
- Maximum `per_page`: 100
- Default `per_page`: 20

---

## Webhooks

Aether can send webhooks for important events.

### Webhook Events

| Event | Description |
|-------|-------------|
| `agent.created` | Agent was created |
| `agent.started` | Agent started successfully |
| `agent.stopped` | Agent stopped |
| `agent.failed` | Agent failed to start or crashed |
| `agent.deleted` | Agent was deleted |
| `quota.warning` | Approaching quota limit (80%) |
| `quota.exceeded` | Quota limit exceeded |

### Webhook Payload

```json
{
  "event": "agent.started",
  "timestamp": "2026-02-09T10:01:30Z",
  "data": {
    "agent_id": "agent-abc123",
    "tenant_id": "tenant-123",
    "name": "my-agent",
    "status": "running"
  },
  "signature": "sha256=abc123..."
}
```

### Verifying Webhooks

```python
import hmac
import hashlib

def verify_webhook(payload, signature, secret):
    expected = hmac.new(
        secret.encode(),
        payload.encode(),
        hashlib.sha256
    ).hexdigest()

    return hmac.compare_digest(
        f"sha256={expected}",
        signature
    )
```

---

## SDKs

Official SDKs are available for popular languages:

- **Python:** `pip install aether-client`
- **Go:** `go get github.com/dnakitare/aether-go-client`
- **Node.js:** `npm install @aether/client`
- **Java:** Maven/Gradle (see docs)

### Python Example

```python
from aether import Client

client = Client(api_key="aether_sk_...")

# Create agent
agent = client.agents.create(
    name="my-agent",
    image="python:3.11-slim",
    resources={
        "cpu_count": 2,
        "memory_mb": 4096,
        "disk_mb": 20480
    }
)

# Start agent
agent.start()

# Get logs
for line in agent.logs(follow=True):
    print(line)

# Stop agent
agent.stop()
```

---

## Support

- **Documentation:** https://docs.aether.example.com
- **API Status:** https://status.aether.example.com
- **Support:** support@aether.example.com
- **GitHub:** https://github.com/dnakitare/aether

---

**Last Updated:** 2026-02-09
**API Version:** 1.0.0
