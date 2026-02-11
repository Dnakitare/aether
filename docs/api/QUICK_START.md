# Aether API Quick Start

Get started with Aether in under 5 minutes.

---

## Prerequisites

- An Aether account (sign up at https://aether.example.com)
- API key or credentials
- `curl` or HTTP client of your choice

---

## Step 1: Authentication

### Get an API Key

Log in to the dashboard and create an API key:

```bash
curl -X POST https://api.aether.example.com/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "your-email@example.com",
    "password": "your-password"
  }'
```

Response:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2026-02-10T10:00:00Z"
}
```

**Save your token:**
```bash
export AETHER_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

---

## Step 2: Create Your First Agent

Create an agent running Python:

```bash
curl -X POST https://api.aether.example.com/v1/agents \
  -H "Authorization: Bearer $AETHER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-first-agent",
    "image": "python:3.11-slim",
    "resources": {
      "cpu_count": 1,
      "memory_mb": 512,
      "disk_mb": 2048
    }
  }'
```

Response:
```json
{
  "id": "agent-abc123",
  "name": "my-first-agent",
  "status": "pending",
  "created_at": "2026-02-09T10:00:00Z"
}
```

**Save the agent ID:**
```bash
export AGENT_ID="agent-abc123"
```

---

## Step 3: Start the Agent

```bash
curl -X POST "https://api.aether.example.com/v1/agents/$AGENT_ID/start" \
  -H "Authorization: Bearer $AETHER_TOKEN"
```

Response:
```json
{
  "id": "agent-abc123",
  "status": "starting",
  "message": "Agent start initiated"
}
```

**Wait a few seconds for the agent to start.**

---

## Step 4: Check Agent Status

```bash
curl "https://api.aether.example.com/v1/agents/$AGENT_ID" \
  -H "Authorization: Bearer $AETHER_TOKEN"
```

Response:
```json
{
  "id": "agent-abc123",
  "name": "my-first-agent",
  "status": "running",
  "started_at": "2026-02-09T10:01:30Z",
  "metrics": {
    "cpu_usage_percent": 5.2,
    "memory_usage_mb": 128
  }
}
```

---

## Step 5: Execute a Command

Run Python code inside your agent:

```bash
curl -X POST "https://api.aether.example.com/v1/agents/$AGENT_ID/exec" \
  -H "Authorization: Bearer $AETHER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "command": ["python", "-c", "print(\"Hello from Aether!\")"]
  }'
```

Response:
```json
{
  "exit_code": 0,
  "stdout": "Hello from Aether!\n",
  "stderr": "",
  "duration_ms": 125
}
```

---

## Step 6: View Logs

```bash
curl "https://api.aether.example.com/v1/agents/$AGENT_ID/logs?tail=20" \
  -H "Authorization: Bearer $AETHER_TOKEN"
```

Response:
```
2026-02-09T10:01:35Z [INFO] Agent started
2026-02-09T10:01:36Z [INFO] Ready to receive commands
```

---

## Step 7: Stop the Agent

```bash
curl -X POST "https://api.aether.example.com/v1/agents/$AGENT_ID/stop" \
  -H "Authorization: Bearer $AETHER_TOKEN"
```

---

## Step 8: Clean Up

Delete the agent when you're done:

```bash
curl -X DELETE "https://api.aether.example.com/v1/agents/$AGENT_ID" \
  -H "Authorization: Bearer $AETHER_TOKEN"
```

---

## Complete Example Script

Save this as `aether-demo.sh`:

```bash
#!/bin/bash
set -e

# Configuration
API_URL="https://api.aether.example.com/v1"
EMAIL="your-email@example.com"
PASSWORD="your-password"

echo "==> Logging in..."
TOKEN=$(curl -s -X POST "$API_URL/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$EMAIL\",\"password\":\"$PASSWORD\"}" \
  | jq -r '.token')

echo "==> Creating agent..."
AGENT_ID=$(curl -s -X POST "$API_URL/agents" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "demo-agent",
    "image": "python:3.11-slim",
    "resources": {
      "cpu_count": 1,
      "memory_mb": 512,
      "disk_mb": 2048
    }
  }' | jq -r '.id')

echo "Agent ID: $AGENT_ID"

echo "==> Starting agent..."
curl -s -X POST "$API_URL/agents/$AGENT_ID/start" \
  -H "Authorization: Bearer $TOKEN" > /dev/null

echo "==> Waiting for agent to start..."
sleep 5

echo "==> Executing command..."
curl -s -X POST "$API_URL/agents/$AGENT_ID/exec" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "command": ["python", "-c", "import sys; print(f\"Python {sys.version}\")"]
  }' | jq '.stdout'

echo "==> Stopping agent..."
curl -s -X POST "$API_URL/agents/$AGENT_ID/stop" \
  -H "Authorization: Bearer $TOKEN" > /dev/null

echo "==> Cleaning up..."
curl -s -X DELETE "$API_URL/agents/$AGENT_ID" \
  -H "Authorization: Bearer $TOKEN" > /dev/null

echo "==> Done!"
```

Run it:
```bash
chmod +x aether-demo.sh
./aether-demo.sh
```

---

## Next Steps

### Learn More

- [Full API Reference](./API_REFERENCE.md)
- [Authentication Guide](./AUTHENTICATION.md)
- [Best Practices](./BEST_PRACTICES.md)
- [Python SDK Guide](./sdks/PYTHON.md)

### Example Use Cases

- [Machine Learning Workloads](../examples/ml-workload.md)
- [Data Processing Pipeline](../examples/data-pipeline.md)
- [Web Scraping](../examples/web-scraper.md)
- [Automated Testing](../examples/test-runner.md)

### Try Advanced Features

- **Auto-scaling:** Automatically scale agents based on load
- **Secrets management:** Inject secrets from Vault
- **Checkpointing:** Save and restore agent state
- **Monitoring:** Set up metrics and alerts

---

## Troubleshooting

### Common Issues

**"401 Unauthorized"**
- Check your token hasn't expired
- Verify the token is included in the Authorization header
- Ensure you're using `Bearer` prefix

**"403 Forbidden"**
- Check your API key has the required scopes
- Verify you're not trying to access another tenant's resources

**"429 Too Many Requests"**
- You've exceeded your rate limit
- Wait for the time specified in `Retry-After` header
- Consider upgrading your tier for higher limits

**Agent stuck in "starting" status**
- Check agent logs for errors
- Verify the image exists and is accessible
- Ensure you haven't exceeded resource quotas

**"507 Insufficient Storage"**
- You've reached your quota limits
- Delete unused agents to free up resources
- Upgrade your plan for higher quotas

### Getting Help

- **Documentation:** https://docs.aether.example.com
- **Community Forum:** https://community.aether.example.com
- **Support Email:** support@aether.example.com
- **Status Page:** https://status.aether.example.com

---

**Last Updated:** 2026-02-09
