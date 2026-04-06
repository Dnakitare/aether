# Production Deployment Guide

This guide covers deploying Aether Runtime to production environments.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Infrastructure Requirements](#infrastructure-requirements)
- [Deployment Options](#deployment-options)
- [Docker Deployment](#docker-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [AWS Deployment with Terraform](#aws-deployment-with-terraform)
- [Configuration](#configuration)
- [Security](#security)
- [High Availability](#high-availability)
- [Monitoring](#monitoring)
- [Backup & Recovery](#backup--recovery)

## Prerequisites

Before deploying Aether to production, ensure you have:

- **Kubernetes cluster** (1.24+) or **Docker Swarm** environment
- **PostgreSQL** (15+) - for persistent state
- **Redis** (7+) - for caching and queues
- **Storage** - for agent logs and checkpoints
- **Load Balancer** - for distributing traffic
- **Monitoring** - Prometheus & Grafana
- **Tracing** - Jaeger or compatible OTLP endpoint

## Infrastructure Requirements

### Minimum Production Setup

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| **API Server** | 2 vCPU, 4GB RAM | 4 vCPU, 8GB RAM |
| **Scheduler** | 2 vCPU, 4GB RAM | 4 vCPU, 8GB RAM |
| **PostgreSQL** | 2 vCPU, 4GB RAM, 100GB SSD | 4 vCPU, 16GB RAM, 500GB SSD |
| **Redis** | 2 vCPU, 2GB RAM | 4 vCPU, 8GB RAM |
| **Storage** | 100GB | 1TB+ |

### Scaling Guidelines

- **1,000 agents**: 2 API servers, 1 scheduler
- **5,000 agents**: 4 API servers, 2 schedulers
- **10,000+ agents**: Scale horizontally, use distributed scheduler

## Deployment Options

Aether supports three primary deployment methods:

1. **Docker Compose** - Development and small deployments
2. **Kubernetes with Helm** - Production deployments (recommended)
3. **AWS with Terraform** - Managed cloud infrastructure

## Docker Deployment

### Using Docker Compose

```bash
# Clone repository
git clone https://github.com/dnakitare/aether.git
cd aether

# Configure environment
cp .env.example .env
# Edit .env with your production settings

# Start services
docker-compose up -d

# Verify deployment
docker-compose ps
curl http://localhost:8080/health
```

### Production Docker Compose

```yaml
version: '3.9'

services:
  aether:
    image: ghcr.io/dnakitare/aether:0.2.0-beta
    ports:
      - "8080:8080"
    environment:
      AETHER_DB_HOST: postgres
      AETHER_REDIS_HOST: redis
      AETHER_TRACING_ENABLED: "true"
      AETHER_TRACING_ENDPOINT: "http://jaeger:4318"
    depends_on:
      - postgres
      - redis
    restart: unless-stopped
    deploy:
      replicas: 2
      resources:
        limits:
          cpus: '4'
          memory: 8G
        reservations:
          cpus: '2'
          memory: 4G

  postgres:
    image: postgres:15-alpine
    volumes:
      - postgres_data:/var/lib/postgresql/data
    environment:
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    restart: unless-stopped

  redis:
    image: redis:7-alpine
    command: redis-server --requirepass ${REDIS_PASSWORD}
    restart: unless-stopped

volumes:
  postgres_data:
```

## Kubernetes Deployment

### Using Helm (Recommended)

```bash
# Add Helm repository (when available)
helm repo add aether https://dnakitare.github.io/helm-charts
helm repo update

# Install with Helm
helm install aether aether/aether \
  --namespace aether \
  --create-namespace \
  --set apiServer.replicaCount=2 \
  --set postgresql.enabled=true \
  --set redis.enabled=true \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=aether.example.com

# Or install from local chart
cd helm/aether
helm install aether . \
  --namespace aether \
  --create-namespace \
  --values values-production.yaml
```

### Production values.yaml

```yaml
# helm/aether/values-production.yaml
apiServer:
  replicaCount: 3
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70

  resources:
    requests:
      cpu: 2000m
      memory: 4Gi
    limits:
      cpu: 4000m
      memory: 8Gi

scheduler:
  replicaCount: 2
  persistence:
    enabled: true
    size: 50Gi
    storageClass: fast-ssd

postgresql:
  enabled: true
  primary:
    persistence:
      size: 500Gi
    resources:
      requests:
        cpu: 2000m
        memory: 8Gi

redis:
  enabled: true
  master:
    persistence:
      size: 10Gi

ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
  hosts:
    - host: aether.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: aether-tls
      hosts:
        - aether.example.com

monitoring:
  serviceMonitor:
    enabled: true
  grafana:
    dashboardsEnabled: true
```

### Verify Deployment

```bash
# Check pods
kubectl get pods -n aether

# Check services
kubectl get svc -n aether

# Check ingress
kubectl get ingress -n aether

# Test health endpoint
curl https://aether.example.com/health

# View logs
kubectl logs -n aether -l app=aether-api -f
```

## AWS Deployment with Terraform

### Quick Start

```bash
cd terraform/aws

# Configure AWS credentials
export AWS_PROFILE=production
export AWS_REGION=us-east-1

# Initialize Terraform
terraform init

# Review plan
terraform plan -var-file=production.tfvars

# Deploy
terraform apply -var-file=production.tfvars
```

### Production terraform.tfvars

```hcl
# terraform/aws/production.tfvars
environment = "production"
aws_region  = "us-east-1"

# VPC Configuration
vpc_cidr           = "10.0.0.0/16"
availability_zones = ["us-east-1a", "us-east-1b", "us-east-1c"]
enable_nat_gateway = true
single_nat_gateway = false  # Multi-AZ NAT for HA

# ECS Configuration
ecs_cluster_name      = "aether-prod"
api_server_count      = 3
api_server_cpu        = 2048
api_server_memory     = 4096
enable_auto_scaling   = true
min_capacity          = 2
max_capacity          = 10

# Database Configuration
db_instance_class     = "db.r6g.xlarge"
db_allocated_storage  = 500
db_multi_az          = true
db_backup_retention  = 7

# Redis Configuration
redis_node_type      = "cache.r6g.large"
redis_num_replicas   = 2
redis_multi_az       = true

# Security
enable_encryption    = true
allowed_cidr_blocks  = ["10.0.0.0/8"]  # Adjust for your network

# Tags
tags = {
  Environment = "production"
  Project     = "aether"
  ManagedBy   = "terraform"
}
```

### Post-Deployment

```bash
# Get outputs
terraform output

# Get ALB endpoint
ALB_ENDPOINT=$(terraform output -raw alb_endpoint)
curl http://$ALB_ENDPOINT/health

# Get database endpoint
DB_ENDPOINT=$(terraform output -raw db_endpoint)
```

## Configuration

### Environment Variables

```bash
# API Configuration
AETHER_API_ADDRESS=":8080"
AETHER_ENABLE_AUTH="true"
AETHER_ENABLE_CORS="false"

# Database
AETHER_DB_HOST="postgres.example.com"
AETHER_DB_PORT="5432"
AETHER_DB_NAME="aether"
AETHER_DB_USER="aether"
AETHER_DB_PASSWORD="<secure-password>"
AETHER_DB_MAX_CONNECTIONS="100"

# Redis
AETHER_REDIS_HOST="redis.example.com"
AETHER_REDIS_PORT="6379"
AETHER_REDIS_PASSWORD="<secure-password>"

# Tracing
AETHER_TRACING_ENABLED="true"
AETHER_TRACING_ENDPOINT="https://jaeger.example.com:4318"
AETHER_TRACING_SERVICE_NAME="aether"

# Logging
AETHER_LOG_LEVEL="info"
AETHER_LOG_FORMAT="json"

# Performance
AETHER_MAX_CONCURRENT_AGENTS="1000"
AETHER_SCHEDULER_QUEUE_SIZE="10000"
```

### Configuration Files

For advanced configuration, use a config file:

```yaml
# /etc/aether/config.yaml
api:
  address: ":8080"
  read_timeout: 30s
  write_timeout: 30s
  enable_auth: true
  enable_cors: false

database:
  host: postgres.example.com
  port: 5432
  name: aether
  user: aether
  max_connections: 100
  max_idle_connections: 10
  connection_timeout: 10s

redis:
  host: redis.example.com
  port: 6379
  max_retries: 3
  pool_size: 100

tracing:
  enabled: true
  endpoint: https://jaeger.example.com:4318
  service_name: aether
  sampling_rate: 0.1

logging:
  level: info
  format: json
  output: stdout
```

## Security

### Authentication

Enable JWT authentication in production:

```bash
# Generate JWT secret
AETHER_JWT_SECRET=$(openssl rand -base64 32)

# Set environment variable
export AETHER_JWT_SECRET="$AETHER_JWT_SECRET"
export AETHER_ENABLE_AUTH="true"
```

### TLS/SSL

Configure TLS for API endpoints:

```yaml
# Kubernetes Ingress
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: aether
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
    - hosts:
        - aether.example.com
      secretName: aether-tls
```

### Network Security

- Use **private subnets** for database and cache
- Configure **security groups** to restrict access
- Enable **network policies** in Kubernetes
- Use **VPN or bastion** for administrative access

### Secrets Management

Use a secrets manager for sensitive data:

```bash
# AWS Secrets Manager
aws secretsmanager create-secret \
  --name aether/db-password \
  --secret-string "your-secure-password"

# Kubernetes Secrets
kubectl create secret generic aether-secrets \
  --from-literal=db-password=<password> \
  --from-literal=redis-password=<password> \
  --from-literal=jwt-secret=<secret> \
  -n aether
```

## High Availability

### Multi-Region Setup

```hcl
# Terraform multi-region
module "primary_region" {
  source = "./modules"
  region = "us-east-1"
  # ... configuration
}

module "secondary_region" {
  source = "./modules"
  region = "us-west-2"
  # ... configuration
}
```

### Database Replication

```yaml
# PostgreSQL with read replicas
postgresql:
  replication:
    enabled: true
    readReplicas: 2
```

### Health Checks

Configure health checks for load balancers:

```yaml
# Kubernetes liveness/readiness probes
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 5
  failureThreshold: 2
```

## Monitoring

### Metrics

Aether exposes Prometheus metrics at `/metrics`:

```yaml
# Prometheus scrape config
scrape_configs:
  - job_name: 'aether'
    static_configs:
      - targets: ['aether:8080']
    metrics_path: '/metrics'
```

### Dashboards

Import pre-configured Grafana dashboards:

```bash
# Located in deployments/grafana/dashboards/
- system-overview.json
- agent-metrics.json
- scheduler-performance.json
- api-latency.json
```

### Alerting

Configure alerts for critical metrics:

```yaml
# Prometheus alerts
groups:
  - name: aether
    rules:
      - alert: HighErrorRate
        expr: rate(aether_requests_total{status="5xx"}[5m]) > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High error rate detected"
```

## Backup & Recovery

### Automated Backups

```bash
# Schedule daily backups
aether backup create --schedule "0 2 * * *"

# Configure retention
aether backup cleanup --retention-days 30
```

### Disaster Recovery

```bash
# Initiate failover
aether dr failover --to secondary-region

# Test DR readiness
aether dr test-failover
```

### Restore Process

```bash
# List available backups
aether backup list

# Restore from backup
aether restore --backup-id backup-20260215-120000

# Verify restore
aether verify --backup-id backup-20260215-120000
```

## Troubleshooting

See the [Troubleshooting Guide](./TROUBLESHOOTING.md) for common issues and solutions.

## Next Steps

- Configure [Observability](./OBSERVABILITY.md)
- Review [Security Best Practices](./SECURITY.md)
- Set up [Disaster Recovery](./DISASTER_RECOVERY.md)

## Support

- **Documentation**: https://dnakitare.github.io/docs
- **GitHub Issues**: https://github.com/dnakitare/aether/issues
- **Community**: https://discord.gg/aether

---

**Last Updated**: February 15, 2026
**Version**: 0.2.0-beta
