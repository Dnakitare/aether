# API Documentation

Complete reference for the Aether Runtime HTTP API.

## Table of Contents

- [Overview](#overview)
- [Authentication](#authentication)
- [Base URL](#base-url)
- [Response Format](#response-format)
- [Error Handling](#error-handling)
- [Rate Limiting](#rate-limiting)
- [Pagination](#pagination)
- [Endpoints](#endpoints)
  - [Health & Status](#health--status)
  - [Agents](#agents)
  - [Metrics](#metrics)

## Overview

The Aether API is a RESTful HTTP API that allows you to manage AI agents programmatically.

**Features:**
- RESTful design
- JSON request/response
- JWT authentication
- Pagination support
- Bulk operations
- Server-Sent Events (SSE) for streaming
- OpenAPI/Swagger compatible

## Authentication

### JWT Authentication

Production deployments should enable JWT authentication:

```bash
# Set environment variable
export AETHER_ENABLE_AUTH=true
export AETHER_JWT_SECRET=<your-secret>
```

### Obtaining a Token

```bash
# Login (example - actual implementation may vary)
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "user@example.com",
    "password": "password"
  }'

# Response
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_at": "2026-02-16T10:00:00Z"
}
```

### Using the Token

Include the JWT token in the `Authorization` header:

```bash
curl -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIs..." \
  http://localhost:8080/agents
```

## Base URL

**Development:**
```
http://localhost:8080
```

**Production:**
```
https://aether.example.com
```

## Response Format

All API responses use JSON format.

### Success Response

```json
{
  "id": "agent-123",
  "name": "My Agent",
  "status": "running",
  "created_at": "2026-02-15T10:00:00Z"
}
```

### Paginated Response

```json
{
  "data": [...],
  "pagination": {
    "total": 100,
    "page": 1,
    "page_size": 20,
    "total_pages": 5
  }
}
```

## Error Handling

### Error Response Format

Errors follow RFC 7807 Problem Details:

```json
{
  "type": "https://aether.example.com/errors/validation-error",
  "title": "Validation Error",
  "status": 400,
  "detail": "Invalid request body",
  "instance": "/agents",
  "errors": [
    {
      "field": "name",
      "message": "Name is required",
      "code": "REQUIRED_FIELD"
    }
  ]
}
```

### HTTP Status Codes

| Status | Meaning |
|--------|---------|
| 200 | Success |
| 201 | Created |
| 204 | No Content |
| 400 | Bad Request |
| 401 | Unauthorized |
| 403 | Forbidden |
| 404 | Not Found |
| 409 | Conflict |
| 429 | Too Many Requests |
| 500 | Internal Server Error |
| 503 | Service Unavailable |

### Error Codes

| Code | Description |
|------|-------------|
| `REQUIRED_FIELD` | Required field missing |
| `INVALID_FORMAT` | Invalid field format |
| `DUPLICATE_RESOURCE` | Resource already exists |
| `NOT_FOUND` | Resource not found |
| `UNAUTHORIZED` | Authentication required |
| `FORBIDDEN` | Insufficient permissions |
| `RATE_LIMIT_EXCEEDED` | Too many requests |

## Rate Limiting

**Default Limits:**
- 100 requests per minute per IP
- 1000 requests per hour per user

**Headers:**
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1644931200
```

**Rate Limit Exceeded Response:**
```json
{
  "type": "https://aether.example.com/errors/rate-limit",
  "title": "Rate Limit Exceeded",
  "status": 429,
  "detail": "Too many requests",
  "retry_after": 60
}
```

## Pagination

### Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | integer | 1 | Page number |
| `page_size` | integer | 20 | Items per page |
| `offset` | integer | 0 | Number of items to skip |

### Example

```bash
# Page-based
curl "http://localhost:8080/agents?page=2&page_size=50"

# Offset-based
curl "http://localhost:8080/agents?offset=100&page_size=50"
```

### Response

```json
{
  "data": [...],
  "pagination": {
    "total": 500,
    "page": 2,
    "page_size": 50,
    "total_pages": 10,
    "has_next": true,
    "has_prev": true
  }
}
```

## Endpoints

### Health & Status

#### GET /health

Check service health.

**Response:**
```json
{
  "status": "healthy",
  "version": "0.2.0-beta",
  "uptime_seconds": 86400,
  "checks": {
    "database": "healthy",
    "redis": "healthy",
    "scheduler": "healthy"
  }
}
```

#### GET /ready

Check if service is ready to accept requests.

**Response:**
```json
{
  "ready": true
}
```

#### GET /metrics

Prometheus metrics endpoint.

**Response:**
```
# HELP aether_http_requests_total Total HTTP requests
# TYPE aether_http_requests_total counter
aether_http_requests_total{method="GET",path="/agents",status="200"} 1234
...
```

### Agents

#### POST /agents

Create a new agent.

**Request:**
```json
{
  "name": "My Agent",
  "image": "python:3.11",
  "command": ["python", "app.py"],
  "env": {
    "API_KEY": "secret"
  },
  "resources": {
    "cpu": "1000m",
    "memory": "2Gi"
  }
}
```

**Response (201):**
```json
{
  "id": "agent-abc123",
  "name": "My Agent",
  "status": "pending",
  "image": "python:3.11",
  "created_at": "2026-02-15T10:00:00Z",
  "resources": {
    "cpu": "1000m",
    "memory": "2Gi"
  }
}
```

#### GET /agents

List all agents.

**Query Parameters:**
- `page` (integer): Page number
- `page_size` (integer): Items per page
- `status` (string): Filter by status (running, pending, failed)

**Example:**
```bash
curl "http://localhost:8080/agents?page=1&page_size=20&status=running"
```

**Response (200):**
```json
{
  "data": [
    {
      "id": "agent-1",
      "name": "Agent 1",
      "status": "running",
      "created_at": "2026-02-15T10:00:00Z"
    }
  ],
  "pagination": {
    "total": 100,
    "page": 1,
    "page_size": 20,
    "total_pages": 5
  }
}
```

#### GET /agents/{id}

Get agent details.

**Response (200):**
```json
{
  "id": "agent-abc123",
  "name": "My Agent",
  "status": "running",
  "image": "python:3.11",
  "created_at": "2026-02-15T10:00:00Z",
  "started_at": "2026-02-15T10:00:05Z",
  "resources": {
    "cpu": "1000m",
    "memory": "2Gi"
  },
  "metadata": {
    "node": "worker-1",
    "pod": "agent-abc123-xyz"
  }
}
```

**Response (404):**
```json
{
  "type": "https://aether.example.com/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "Agent not found",
  "instance": "/agents/nonexistent"
}
```

#### DELETE /agents/{id}

Delete an agent.

**Response (204):**
```
No content
```

**Response (404):**
```json
{
  "type": "https://aether.example.com/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "Agent not found"
}
```

#### GET /agents/{id}/logs

Stream agent logs via Server-Sent Events.

**Query Parameters:**
- `follow` (boolean): Follow log output
- `tail` (integer): Number of lines from end

**Example:**
```bash
curl -N "http://localhost:8080/agents/agent-123/logs?follow=true&tail=100"
```

**Response (SSE):**
```
data: {"timestamp":"2026-02-15T10:00:00Z","level":"info","msg":"Starting agent"}

data: {"timestamp":"2026-02-15T10:00:01Z","level":"info","msg":"Agent ready"}
```

#### POST /agents/{id}/restart

Restart an agent.

**Response (200):**
```json
{
  "id": "agent-abc123",
  "status": "restarting",
  "message": "Agent restart initiated"
}
```

### Bulk Operations

#### POST /agents/bulk

Create multiple agents.

**Request:**
```json
{
  "agents": [
    {
      "name": "Agent 1",
      "image": "python:3.11"
    },
    {
      "name": "Agent 2",
      "image": "python:3.11"
    }
  ]
}
```

**Response (201):**
```json
{
  "created": 2,
  "failed": 0,
  "results": [
    {
      "id": "agent-1",
      "status": "created"
    },
    {
      "id": "agent-2",
      "status": "created"
    }
  ]
}
```

#### DELETE /agents/bulk

Delete multiple agents.

**Request:**
```json
{
  "ids": ["agent-1", "agent-2", "agent-3"]
}
```

**Response (200):**
```json
{
  "deleted": 3,
  "failed": 0,
  "results": [
    {
      "id": "agent-1",
      "status": "deleted"
    },
    {
      "id": "agent-2",
      "status": "deleted"
    },
    {
      "id": "agent-3",
      "status": "deleted"
    }
  ]
}
```

## Code Examples

### cURL

```bash
# Create agent
curl -X POST http://localhost:8080/agents \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "My Agent",
    "image": "python:3.11"
  }'

# List agents
curl http://localhost:8080/agents?page=1&page_size=20 \
  -H "Authorization: Bearer $TOKEN"

# Get agent
curl http://localhost:8080/agents/agent-123 \
  -H "Authorization: Bearer $TOKEN"

# Delete agent
curl -X DELETE http://localhost:8080/agents/agent-123 \
  -H "Authorization: Bearer $TOKEN"
```

### Python

```python
import requests

BASE_URL = "http://localhost:8080"
TOKEN = "your-jwt-token"

headers = {
    "Authorization": f"Bearer {TOKEN}",
    "Content-Type": "application/json"
}

# Create agent
response = requests.post(
    f"{BASE_URL}/agents",
    headers=headers,
    json={
        "name": "My Agent",
        "image": "python:3.11"
    }
)
agent = response.json()
print(f"Created agent: {agent['id']}")

# List agents
response = requests.get(
    f"{BASE_URL}/agents",
    headers=headers,
    params={"page": 1, "page_size": 20}
)
agents = response.json()
print(f"Total agents: {agents['pagination']['total']}")

# Get agent
response = requests.get(
    f"{BASE_URL}/agents/{agent['id']}",
    headers=headers
)
agent_details = response.json()
print(f"Agent status: {agent_details['status']}")

# Delete agent
response = requests.delete(
    f"{BASE_URL}/agents/{agent['id']}",
    headers=headers
)
print(f"Agent deleted: {response.status_code == 204}")
```

### JavaScript/Node.js

```javascript
const axios = require('axios');

const BASE_URL = 'http://localhost:8080';
const TOKEN = 'your-jwt-token';

const client = axios.create({
  baseURL: BASE_URL,
  headers: {
    'Authorization': `Bearer ${TOKEN}`,
    'Content-Type': 'application/json'
  }
});

// Create agent
const createAgent = async () => {
  const { data } = await client.post('/agents', {
    name: 'My Agent',
    image: 'python:3.11'
  });
  console.log(`Created agent: ${data.id}`);
  return data.id;
};

// List agents
const listAgents = async () => {
  const { data } = await client.get('/agents', {
    params: { page: 1, page_size: 20 }
  });
  console.log(`Total agents: ${data.pagination.total}`);
  return data.data;
};

// Get agent
const getAgent = async (id) => {
  const { data } = await client.get(`/agents/${id}`);
  console.log(`Agent status: ${data.status}`);
  return data;
};

// Delete agent
const deleteAgent = async (id) => {
  await client.delete(`/agents/${id}`);
  console.log(`Agent ${id} deleted`);
};
```

### Go

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
)

const (
    BaseURL = "http://localhost:8080"
    Token   = "your-jwt-token"
)

type Agent struct {
    ID     string `json:"id"`
    Name   string `json:"name"`
    Status string `json:"status"`
}

func createAgent(name, image string) (*Agent, error) {
    payload := map[string]string{
        "name":  name,
        "image": image,
    }
    body, _ := json.Marshal(payload)

    req, _ := http.NewRequest("POST", BaseURL+"/agents", bytes.NewBuffer(body))
    req.Header.Set("Authorization", "Bearer "+Token)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var agent Agent
    json.NewDecoder(resp.Body).Decode(&agent)
    return &agent, nil
}

func listAgents() ([]Agent, error) {
    req, _ := http.NewRequest("GET", BaseURL+"/agents", nil)
    req.Header.Set("Authorization", "Bearer "+Token)

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result struct {
        Data []Agent `json:"data"`
    }
    json.NewDecoder(resp.Body).Decode(&result)
    return result.Data, nil
}
```

## Webhooks

Aether supports webhooks for event notifications:

```bash
# Configure webhook
curl -X POST http://localhost:8080/webhooks \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "url": "https://example.com/webhook",
    "events": ["agent.created", "agent.failed"]
  }'
```

**Webhook Payload:**
```json
{
  "event": "agent.created",
  "timestamp": "2026-02-15T10:00:00Z",
  "data": {
    "id": "agent-123",
    "name": "My Agent",
    "status": "running"
  }
}
```

## OpenAPI/Swagger

An OpenAPI specification is available at:
```
http://localhost:8080/swagger.json
```

View interactive API documentation:
```
http://localhost:8080/swagger-ui
```

## Support

- **Documentation**: https://dnakitare.github.io/docs
- **API Reference**: https://dnakitare.github.io/api
- **GitHub**: https://github.com/dnakitare/aether
- **Community**: https://discord.gg/aether

---

**Last Updated**: February 15, 2026
**Version**: 0.2.0-beta
