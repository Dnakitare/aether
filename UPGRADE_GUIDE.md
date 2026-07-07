# Upgrade Guide: Alpha v0.1.0 → Beta v0.2.0

Complete guide for upgrading from Aether Alpha v0.1.0 to Beta v0.2.0.

## Overview

Beta v0.2.0 is **backward compatible** with Alpha v0.1.0. The upgrade process is straightforward, with no breaking changes to core functionality.

**Upgrade Time**: 15-30 minutes
**Downtime Required**: Optional (can be done with rolling updates)

## What's New in Beta

- Distributed tracing (OpenTelemetry/Jaeger)
- Prometheus metrics & Grafana dashboards
- Enhanced CLI with progress indicators
- Checkpoint/Restore functionality
- Production deployment tools (Terraform, Helm)
- Comprehensive documentation

## Pre-Upgrade Checklist

- [ ] Backup your database
- [ ] Review [Beta Release Notes](docs/archive/BETA_RELEASE_NOTES.md)
- [ ] Check current version: `aether version`
- [ ] Note your current configuration
- [ ] Plan maintenance window (optional)

## Backup Procedure

### Database Backup

```bash
# PostgreSQL backup
pg_dump -h localhost -U aether aether > aether_alpha_backup.sql
```

### Configuration Backup

```bash
# Backup environment variables
env | grep AETHER_ > aether_alpha_env.txt

# Backup config file (if using)
cp /etc/aether/config.yaml /etc/aether/config.yaml.alpha
```

## Upgrade Steps

### Option 1: Docker Deployment

#### 1. Pull New Image

```bash
# Pull Beta image
docker pull ghcr.io/dnakitare/aether:0.2.0-beta

# Or build locally
git checkout beta/v0.2.0
docker build -t aether:0.2.0-beta .
```

#### 2. Update docker-compose.yml

```yaml
# Update image version
services:
  aether:
    image: ghcr.io/dnakitare/aether:0.2.0-beta
    # ... rest of config
```

#### 3. Add Optional Observability Stack

```yaml
# Add to docker-compose.yml
services:
  # ... existing services ...

  jaeger:
    image: jaegertracing/all-in-one:1.51
    ports:
      - "16686:16686"  # UI
      - "4318:4318"    # OTLP HTTP
    environment:
      COLLECTOR_OTLP_ENABLED: "true"

  prometheus:
    image: prom/prometheus:v2.48.0
    ports:
      - "9090:9090"
    volumes:
      - ./deployments/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml

  grafana:
    image: grafana/grafana:10.2.2
    ports:
      - "3000:3000"
    volumes:
      - ./deployments/grafana/dashboards:/etc/grafana/provisioning/dashboards
```

#### 4. Update Environment Variables

```bash
# Add tracing configuration (optional but recommended)
export AETHER_TRACING_ENABLED=true
export AETHER_TRACING_ENDPOINT=http://jaeger:4318
export AETHER_TRACING_SERVICE_NAME=aether

# Keep existing configuration
export AETHER_DB_HOST=postgres
export AETHER_REDIS_HOST=redis
# ... other vars ...
```

#### 5. Deploy

```bash
# Stop current version
docker-compose down

# Start Beta version
docker-compose up -d

# Verify deployment
docker-compose ps
curl http://localhost:8080/health
```

### Option 2: Kubernetes Deployment

#### 1. Backup Current Deployment

```bash
# Export current deployment
kubectl get deployment aether -n aether -o yaml > aether-alpha-deployment.yaml

# Backup configmaps and secrets
kubectl get configmap -n aether -o yaml > aether-alpha-configmaps.yaml
kubectl get secret -n aether -o yaml > aether-alpha-secrets.yaml
```

#### 2. Using Helm (New in Beta)

```bash
# Clone repository
git clone https://github.com/dnakitare/aether.git
cd aether
git checkout beta/v0.2.0

# Install/Upgrade with Helm
helm upgrade aether ./helm/aether \
  --namespace aether \
  --install \
  --set image.tag=0.2.0-beta \
  --set monitoring.enabled=true \
  --values my-values.yaml
```

#### 3. Manual Kubernetes Upgrade

```bash
# Update image in deployment
kubectl set image deployment/aether \
  aether=ghcr.io/dnakitare/aether:0.2.0-beta \
  -n aether

# Watch rollout
kubectl rollout status deployment/aether -n aether

# Verify
kubectl get pods -n aether
kubectl logs -n aether deployment/aether --tail=50
```

#### 4. Add Observability (Optional)

```bash
# Deploy Prometheus & Grafana (if not already present)
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm install prometheus prometheus-community/kube-prometheus-stack \
  --namespace observability \
  --create-namespace

# Configure ServiceMonitor for Aether
kubectl apply -f helm/aether/templates/servicemonitor.yaml
```

### Option 3: Binary/Source Deployment

#### 1. Download Beta Release

```bash
# Download binary
wget https://github.com/dnakitare/aether/releases/download/v0.2.0-beta/aether-linux-amd64
chmod +x aether-linux-amd64
mv aether-linux-amd64 /usr/local/bin/aether

# Or build from source
git clone https://github.com/dnakitare/aether.git
cd aether
git checkout beta/v0.2.0
make build
sudo make install
```

#### 2. Update Configuration

```bash
# Update config file
sudo vi /etc/aether/config.yaml

# Add tracing section (optional)
tracing:
  enabled: true
  endpoint: http://localhost:4318
  service_name: aether
  sampling_rate: 0.1
```

#### 3. Restart Service

```bash
# Systemd
sudo systemctl restart aether
sudo systemctl status aether

# Verify
aether version
curl http://localhost:8080/health
```

## Configuration Changes

### New Environment Variables

```bash
# Tracing (Optional - for distributed tracing)
AETHER_TRACING_ENABLED=true
AETHER_TRACING_ENDPOINT=http://jaeger:4318
AETHER_TRACING_SERVICE_NAME=aether
AETHER_TRACING_SAMPLING_RATE=0.1

# Metrics (Enabled by default)
# Access at http://localhost:8080/metrics

# CLI (Optional - for better UX)
AETHER_CLI_COLOR=auto
AETHER_CLI_PROGRESS=true
```

### Deprecated Variables

**None** - All Alpha environment variables are still supported.

### Database Schema

No manual migrations required. Aether automatically handles schema updates.

```bash
# Database migrations run automatically on startup
# Check logs for migration status:
docker logs aether-runtime | grep migration
```

## Verification

### Health Check

```bash
# Basic health
curl http://localhost:8080/health

# Expected response
{
  "status": "healthy",
  "version": "0.2.0-beta",
  "uptime_seconds": 123,
  "checks": {
    "database": "healthy",
    "redis": "healthy",
    "scheduler": "healthy"
  }
}
```

### Metrics Endpoint

```bash
# Check metrics are exposed
curl http://localhost:8080/metrics | head -20

# Expected: Prometheus format metrics
# HELP aether_http_requests_total Total HTTP requests
# TYPE aether_http_requests_total counter
aether_http_requests_total{method="GET",path="/health",status="200"} 1
```

### Tracing (if enabled)

```bash
# Create a test agent to generate a trace
curl -X POST http://localhost:8080/agents \
  -H "Content-Type: application/json" \
  -d '{"name":"test-agent","image":"python:3.11"}'

# Open Jaeger UI
open http://localhost:16686

# Search for service: aether
# Look for CreateAgent operations
```

### CLI Features

```bash
# Check new CLI features
aether version

# Test autocomplete (after installation)
aether completion --help

# Test checkpoint commands
aether agent checkpoint --help
```

## Rollback Procedure

If you encounter issues, you can rollback to Alpha:

### Docker Rollback

```bash
# Stop Beta
docker-compose down

# Restore Alpha image
docker-compose.yml:
  image: ghcr.io/dnakitare/aether:0.1.0

# Start Alpha
docker-compose up -d

# Restore database if needed
psql -h localhost -U aether aether < aether_alpha_backup.sql
```

### Kubernetes Rollback

```bash
# Rollback deployment
kubectl rollout undo deployment/aether -n aether

# Or set specific revision
kubectl rollout undo deployment/aether -n aether --to-revision=1

# Verify
kubectl get pods -n aether
```

## Troubleshooting

### Issue: Tracing not working

**Solution**:
```bash
# Verify Jaeger is running
curl http://localhost:4318/v1/traces

# Check environment variables
echo $AETHER_TRACING_ENABLED
echo $AETHER_TRACING_ENDPOINT

# View logs for tracing errors
docker logs aether-runtime | grep -i trace
```

### Issue: Metrics endpoint 404

**Solution**:
```bash
# Metrics are always enabled in Beta
# Verify endpoint:
curl -v http://localhost:8080/metrics

# Check if service is behind a proxy that filters /metrics
```

### Issue: Database connection errors

**Solution**:
```bash
# Beta uses same database as Alpha
# Check connection:
psql -h $AETHER_DB_HOST -U $AETHER_DB_USER -d $AETHER_DB_NAME

# Check logs:
docker logs aether-runtime | grep -i database
```

### Issue: High memory usage

**Solution**:
```bash
# Beta includes observability overhead
# Increase memory limits:
docker-compose.yml:
  services:
    aether:
      deploy:
        resources:
          limits:
            memory: 4G  # Increased from 2G
```

## Post-Upgrade Tasks

### 1. Configure Dashboards

```bash
# Import Grafana dashboards
cd deployments/grafana/dashboards
for dashboard in *.json; do
  curl -X POST http://admin:admin@localhost:3000/api/dashboards/db \
    -H "Content-Type: application/json" \
    -d @"$dashboard"
done
```

### 2. Set Up Alerting

```bash
# Configure Prometheus alerts
cp deployments/prometheus/alerts.yml /etc/prometheus/
# Reload Prometheus config
curl -X POST http://localhost:9090/-/reload
```

### 3. Enable Autocomplete

```bash
# Bash
aether completion bash | sudo tee /etc/bash_completion.d/aether

# Zsh
aether completion zsh | sudo tee /usr/local/share/zsh/site-functions/_aether

# Fish
aether completion fish | sudo tee ~/.config/fish/completions/aether.fish
```

### 4. Review Documentation

- [Production Deployment Guide](docs/guides/PRODUCTION_DEPLOYMENT.md)
- [Observability Guide](docs/guides/OBSERVABILITY.md)
- [Troubleshooting Guide](docs/guides/TROUBLESHOOTING.md)
- [API Reference](docs/guides/API.md)

## Feature Migration

### Using New Checkpoint/Restore

```bash
# Alpha: Manual state management
# Beta: Built-in checkpoint system

# Create checkpoint
aether agent checkpoint create agent-123

# List checkpoints
aether agent checkpoint list agent-123

# Restore from checkpoint
aether agent checkpoint restore agent-123 checkpoint-456
```

### Using Bulk Operations

```bash
# Alpha: Individual agent operations
# Beta: Bulk operations supported

# Bulk create
curl -X POST http://localhost:8080/agents/bulk \
  -d '{
    "agents": [
      {"name": "agent-1", "image": "python:3.11"},
      {"name": "agent-2", "image": "python:3.11"}
    ]
  }'

# Bulk delete
curl -X DELETE http://localhost:8080/agents/bulk \
  -d '{"ids": ["agent-1", "agent-2"]}'
```

## Performance Comparison

### Alpha v0.1.0
- Tested up to 1,000 agents
- Basic monitoring
- Manual deployment

### Beta v0.2.0
- Tested up to 5,000 agents
- Complete observability
- Automated deployment (Terraform, Helm)
- Enhanced CLI experience

## Getting Help

If you encounter issues during upgrade:

1. Check [Troubleshooting Guide](docs/guides/TROUBLESHOOTING.md)
2. Review [GitHub Issues](https://github.com/dnakitare/aether/issues)
3. Ask on [Discord](https://discord.gg/aether)
4. Create new issue with upgrade details

## Feedback

We'd love to hear about your upgrade experience:
- What went smoothly?
- What was challenging?
- Feature requests?

Share feedback: https://github.com/dnakitare/aether/discussions

---

**Last Updated**: February 15, 2026
**Version**: Beta v0.2.0
