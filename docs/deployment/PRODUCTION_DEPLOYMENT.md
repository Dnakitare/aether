# Production Deployment Guide

> **⚠️ ALPHA STATUS - INFRASTRUCTURE INCOMPLETE**
>
> Production deployment tooling is under active development.
> This guide represents the **planned** deployment architecture.
>
> **Current State:**
> - ✅ Basic Terraform for AWS (VPC, networking, security baseline)
> - 🚧 Auto-scaling groups, load balancers (in development)
> - 🚧 Complete RDS and ElastiCache setup (partial)
> - ❌ Azure deployment (not supported; AWS and GCP only)
> - ❌ Kubernetes manifests (planned)
>
> **For production deployments:**
> - Use at your own risk - this is pre-alpha software
> - Expect manual configuration and troubleshooting
> - Consider waiting for beta release (April 2026)
>
> **Local development:** See [Getting Started Local](../GETTING_STARTED_LOCAL.md)

---

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Architecture](#architecture)
- [AWS Deployment](#aws-deployment)
- [GCP Deployment](#gcp-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Configuration](#configuration)
- [Security](#security)
- [Monitoring](#monitoring)
- [Backup & Recovery](#backup--recovery)
- [Scaling](#scaling)
- [Troubleshooting](#troubleshooting)

---

## Overview

This guide covers single-region production deployment of Aether with:
- **Managed data services:** Multi-AZ PostgreSQL and Redis within one region
- **Security:** TLS, secrets management, network isolation
- **Observability:** Metrics, logs, and distributed tracing
- **Scalability:** Auto-scaling from 10 to 10,000+ agents

**Estimated Setup Time:** 2-4 hours
**Required Expertise:** DevOps/SRE experience recommended

---

## Prerequisites

### Infrastructure Requirements

| Component | Minimum | Recommended | Notes |
|-----------|---------|-------------|-------|
| **Compute Nodes** | 3 nodes | 5+ nodes | For agent workloads |
| **Control Plane** | 3 nodes | 3 nodes | For Aether services |
| **Database** | Single instance | Multi-AZ | PostgreSQL 15+ |
| **Cache** | Single instance | Multi-AZ | Redis 7+ |

### Resource Sizing

**Small Deployment (< 100 agents):**
- Control plane: 3x c5.2xlarge (8 vCPU, 16 GB RAM)
- Compute nodes: 3x c5.4xlarge (16 vCPU, 32 GB RAM)
- Database: db.m5.large
- Redis: cache.m5.large
- Total cost: ~$2,500/month (AWS)

**Medium Deployment (100-1,000 agents):**
- Control plane: 3x c5.4xlarge (16 vCPU, 32 GB RAM)
- Compute nodes: 10x c5.9xlarge (36 vCPU, 72 GB RAM)
- Database: db.m5.2xlarge (Multi-AZ)
- Redis: cache.r5.xlarge (Multi-AZ)
- Total cost: ~$15,000/month (AWS)

**Large Deployment (1,000-10,000 agents):**
- Control plane: 5x c5.9xlarge (36 vCPU, 72 GB RAM)
- Compute nodes: 50x c5.9xlarge (36 vCPU, 72 GB RAM)
- Database: db.r5.4xlarge (Multi-AZ)
- Redis: cache.r5.2xlarge (Multi-AZ)
- Total cost: ~$80,000/month (AWS)

### Software Requirements

- Terraform 1.5+
- kubectl 1.28+
- Helm 3.12+
- Docker 24+
- PostgreSQL client 15+

---

## Architecture

### Production Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                       Load Balancer (ALB)                    │
│                   api.aether.example.com                     │
└──────────────────────┬──────────────────────────────────────┘
                       │
         ┌─────────────┼─────────────┐
         │             │             │
    ┌────▼───┐   ┌────▼───┐   ┌────▼───┐
    │ API 1  │   │ API 2  │   │ API 3  │  ← Control Plane
    │ (AZ-a) │   │ (AZ-b) │   │ (AZ-c) │  (in-process scheduler)
    └────┬───┘   └────┬───┘   └────┬───┘
         │             │             │
         └─────────────┼─────────────┘
                       │
    ┌──────────────────▼───────────────┐
    │     Compute Nodes (10-50+)       │
    │  ┌──────┐  ┌──────┐  ┌──────┐   │
    │  │Agent1│  │Agent2│  │Agent3│   │
    │  │ VM   │  │ VM   │  │ VM   │   │
    │  └──────┘  └──────┘  └──────┘   │
    └──────────────────────────────────┘
         │             │
    ┌────▼────┐   ┌────▼────┐
    │PostgreSQL│   │  Redis  │  ← Data Layer
    │ (Multi-AZ│   │(Multi-AZ)
    └──────────┘   └──────────┘
```

### Component Responsibilities

**API Servers (Stateless):**
- Handle HTTP API requests
- JWT authentication & authorization
- Run the in-process scheduler (`aether server`)

**Scheduler (In-process):**
- Agent placement decisions
- Resource allocation
- Node health monitoring

**Compute Nodes (Workers):**
- Run Firecracker microVMs
- Execute agent workloads
- Resource isolation
- Local monitoring

**Data Layer:**
- PostgreSQL: Agent metadata, audit logs
- Redis: State cache, rate limiting, sessions

---

## AWS Deployment

### Step 1: Prepare Infrastructure

**Clone the repository:**
```bash
git clone https://github.com/dnakitare/aether.git
cd aether/deployments/terraform/aws
```

**Configure variables:**
```bash
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars`:
```hcl
# Basic Configuration
environment         = "production"
region             = "us-east-1"
availability_zones = ["us-east-1a", "us-east-1b", "us-east-1c"]

# Networking
vpc_cidr = "10.0.0.0/16"
enable_nat_gateway = true
enable_vpn_gateway = false

# Compute
control_plane_instance_type = "c5.4xlarge"
control_plane_count         = 3
compute_node_instance_type  = "c5.9xlarge"
compute_node_min_count      = 5
compute_node_max_count      = 50

# Database
db_instance_class = "db.m5.2xlarge"
db_multi_az       = true
db_backup_retention_days = 30

# Redis
redis_node_type   = "cache.r5.xlarge"
redis_num_cache_nodes = 3

# Tags
tags = {
  Environment = "production"
  Project     = "aether"
  ManagedBy   = "terraform"
}
```

### Step 2: Initialize Terraform

```bash
# Initialize backend (S3 + DynamoDB for state locking)
terraform init \
  -backend-config="bucket=your-terraform-state-bucket" \
  -backend-config="key=aether/production/terraform.tfstate" \
  -backend-config="region=us-east-1" \
  -backend-config="dynamodb_table=terraform-state-lock"
```

### Step 3: Plan Deployment

```bash
terraform plan -out=production.tfplan
```

Review the plan carefully. Expected resources:
- VPC with 3 public and 3 private subnets
- Security groups
- RDS PostgreSQL (Multi-AZ)
- ElastiCache Redis (Multi-AZ)
- EC2 instances for control plane and compute nodes
- Application Load Balancer
- S3 buckets for backups
- IAM roles and policies
- CloudWatch log groups

### Step 4: Apply Configuration

```bash
terraform apply production.tfplan
```

**This will take 15-20 minutes.**

Save the outputs:
```bash
terraform output -json > terraform-outputs.json
```

### Step 5: Configure DNS

Point your domain to the load balancer:

```bash
ALB_DNS=$(terraform output -raw alb_dns_name)
echo "Create CNAME record: api.aether.example.com -> $ALB_DNS"
```

In Route53 or your DNS provider:
```
api.aether.example.com  CNAME  alb-xyz.us-east-1.elb.amazonaws.com
```

### Step 6: Deploy Aether Services

SSH into a control plane node:
```bash
CONTROL_PLANE_IP=$(terraform output -json | jq -r '.control_plane_ips.value[0]')
ssh -i your-key.pem ec2-user@$CONTROL_PLANE_IP
```

Download Aether:
```bash
wget https://github.com/dnakitare/aether/releases/download/v1.0.0/aether-linux-amd64
chmod +x aether-linux-amd64
sudo mv aether-linux-amd64 /usr/local/bin/aether
```

Create configuration:
```bash
sudo mkdir -p /etc/aether
sudo vim /etc/aether/config.yaml
```

Configuration file:
```yaml
# /etc/aether/config.yaml
server:
  address: ":8080"
  read_timeout: 30s
  write_timeout: 30s

database:
  host: "aether-db.abc123.us-east-1.rds.amazonaws.com"
  port: 5432
  database: "aether"
  user: "aether"
  password: "${DATABASE_PASSWORD}"  # From SSM Parameter Store
  ssl_mode: "require"
  max_connections: 100

redis:
  address: "aether-redis.abc123.cache.amazonaws.com:6379"
  password: "${REDIS_PASSWORD}"  # From SSM Parameter Store
  db: 0
  max_retries: 3

auth:
  jwt_secret: "${JWT_SECRET}"  # From SSM Parameter Store
  token_duration: 1h

scheduler:
  strategy: "bin_packing"
  interval: 5s

observability:
  tracing:
    enabled: true
    endpoint: "jaeger.internal:4317"
    use_tls: true
  metrics:
    enabled: true
    port: 9090

logging:
  level: "info"
  format: "json"
```

### Step 7: Set Up Secrets

Store secrets in AWS Systems Manager Parameter Store:

```bash
# Generate secure passwords
JWT_SECRET=$(openssl rand -base64 32)
DB_PASSWORD=$(openssl rand -base64 32)
REDIS_PASSWORD=$(openssl rand -base64 32)

# Store in Parameter Store (encrypted)
aws ssm put-parameter \
  --name "/aether/production/jwt-secret" \
  --value "$JWT_SECRET" \
  --type "SecureString" \
  --region us-east-1

aws ssm put-parameter \
  --name "/aether/production/database-password" \
  --value "$DB_PASSWORD" \
  --type "SecureString" \
  --region us-east-1

aws ssm put-parameter \
  --name "/aether/production/redis-password" \
  --value "$REDIS_PASSWORD" \
  --type "SecureString" \
  --region us-east-1
```

### Step 8: Initialize Database

Run migrations:
```bash
export DATABASE_URL="postgres://aether:$DB_PASSWORD@aether-db.abc123.us-east-1.rds.amazonaws.com:5432/aether?sslmode=require"
aether migrate up
```

### Step 9: Start Services

Create systemd service file:
```bash
sudo vim /etc/systemd/system/aether.service
```

```ini
[Unit]
Description=Aether Runtime
After=network.target

[Service]
Type=simple
User=aether
Group=aether
WorkingDirectory=/opt/aether
ExecStart=/usr/local/bin/aether serve --config /etc/aether/config.yaml
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
LimitNOFILE=65536

# Environment variables from Parameter Store
EnvironmentFile=/etc/aether/environment

[Install]
WantedBy=multi-user.target
```

Create environment file with secrets:
```bash
# Fetch secrets from Parameter Store
cat > /etc/aether/environment <<EOF
DATABASE_PASSWORD=$(aws ssm get-parameter --name "/aether/production/database-password" --with-decryption --query "Parameter.Value" --output text)
REDIS_PASSWORD=$(aws ssm get-parameter --name "/aether/production/redis-password" --with-decryption --query "Parameter.Value" --output text)
JWT_SECRET=$(aws ssm get-parameter --name "/aether/production/jwt-secret" --with-decryption --query "Parameter.Value" --output text)
EOF
chmod 600 /etc/aether/environment
```

Start the service:
```bash
sudo systemctl daemon-reload
sudo systemctl enable aether
sudo systemctl start aether
sudo systemctl status aether
```

### Step 10: Verify Deployment

Health check:
```bash
curl https://api.aether.example.com/health
```

Expected response:
```json
{
  "status": "healthy"
}
```

Readiness check:
```bash
curl https://api.aether.example.com/readiness
```

Expected response:
```json
{
  "status": "ready",
  "checks": {
    "database": "ok",
    "redis": "ok",
    "scheduler": "ok"
  }
}
```

### Step 11: Set Up Monitoring

**CloudWatch Logs:**
```bash
# Already configured via Terraform
# View logs:
aws logs tail /aws/aether/production --follow
```

**CloudWatch Metrics:**
```bash
# Custom metrics are automatically sent
# View in CloudWatch console
```

**Alarms:**
```bash
# Create alarm for API errors
aws cloudwatch put-metric-alarm \
  --alarm-name "aether-api-errors-high" \
  --alarm-description "Alert when API error rate is high" \
  --metric-name "5xxErrors" \
  --namespace "AWS/ApplicationELB" \
  --statistic "Sum" \
  --period 300 \
  --evaluation-periods 2 \
  --threshold 10 \
  --comparison-operator "GreaterThanThreshold" \
  --alarm-actions "arn:aws:sns:us-east-1:123456789:alerts"
```

### Step 12: Configure Auto-Scaling

Auto-scaling is already configured via Terraform, but you can adjust:

```bash
# Update Auto Scaling Group
aws autoscaling update-auto-scaling-group \
  --auto-scaling-group-name "aether-compute-nodes-production" \
  --min-size 5 \
  --max-size 50 \
  --desired-capacity 10
```

---

## GCP Deployment

### Overview

Similar to AWS but using GCP services:
- Compute Engine for VMs
- Cloud SQL for PostgreSQL
- Memorystore for Redis
- GKE for Kubernetes (optional)
- Cloud Load Balancing
- Cloud Monitoring

### Quick Start

```bash
cd deployments/terraform/gcp

# Configure
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your settings

# Initialize
terraform init

# Deploy
terraform plan
terraform apply

# Outputs
terraform output -json > gcp-outputs.json
```

See `deployments/terraform/gcp/README.md` for detailed instructions.

---

## Kubernetes Deployment

For Kubernetes-based deployment, see dedicated guides:
- [Kubernetes Deployment Guide](./KUBERNETES_DEPLOYMENT.md)
- [Helm Chart Documentation](./HELM_DEPLOYMENT.md)

---

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `AETHER_CONFIG` | Path to config file | `/etc/aether/config.yaml` |
| `DATABASE_URL` | PostgreSQL connection string | - |
| `REDIS_URL` | Redis connection string | - |
| `JWT_SECRET` | JWT signing secret | - |
| `LOG_LEVEL` | Logging level | `info` |
| `ENVIRONMENT` | Environment name | `production` |

### Configuration File

See [Configuration Reference](./CONFIGURATION.md) for all options.

---

## Security

### TLS/SSL

**Enable TLS for all components:**

1. **API Server:**
```yaml
server:
  tls:
    enabled: true
    cert_file: "/etc/aether/certs/server.crt"
    key_file: "/etc/aether/certs/server.key"
```

2. **Database:**
```yaml
database:
  ssl_mode: "require"
  ssl_cert: "/etc/aether/certs/db-client.crt"
  ssl_key: "/etc/aether/certs/db-client.key"
  ssl_root_cert: "/etc/aether/certs/db-ca.crt"
```

### Secrets Management

Use a secrets manager:
- AWS: Systems Manager Parameter Store or Secrets Manager
- GCP: Secret Manager

**Example with AWS Secrets Manager:**
```bash
# Store secret
aws secretsmanager create-secret \
  --name "aether/production/jwt-secret" \
  --secret-string "your-secret-value"

# Retrieve in application
aws secretsmanager get-secret-value \
  --secret-id "aether/production/jwt-secret" \
  --query "SecretString" \
  --output text
```

### Network Security

**Security Groups (AWS) / Firewall Rules:**

```
Control Plane:
  - Allow 443 (HTTPS) from ALB
  - Allow 8080 from ALB
  - Allow 22 (SSH) from bastion only

Compute Nodes:
  - Allow 22 (SSH) from bastion only
  - Allow agent traffic from control plane
  - No public internet access (NAT gateway for outbound)

Database:
  - Allow 5432 from control plane and compute nodes only
  - No public access

Redis:
  - Allow 6379 from control plane and compute nodes only
  - No public access
```

### Access Control

**IAM Roles (AWS):**
- Minimum privilege principle
- Separate roles for control plane, compute nodes, operators
- Use instance profiles, not hardcoded credentials

**RBAC (Kubernetes):**
- Namespace isolation
- Pod security policies
- Network policies

---

## Monitoring

### Metrics

**Key Metrics to Monitor:**

| Metric | Alert Threshold | Description |
|--------|----------------|-------------|
| API Error Rate | > 1% | API 5xx errors |
| API Latency p99 | > 1s | Slow requests |
| Agent Success Rate | < 95% | Failed agent starts |
| CPU Usage | > 80% | High CPU utilization |
| Memory Usage | > 85% | High memory usage |
| Disk Usage | > 80% | Low disk space |
| Database Connections | > 90% of max | Connection pool exhaustion |
| Queue Length | > 1000 | Scheduler backlog |

**Prometheus Queries:**

```promql
# API error rate
rate(aether_api_requests_total{status=~"5.."}[5m]) / rate(aether_api_requests_total[5m])

# Agent success rate
rate(aether_agents_started_total[5m]) / rate(aether_agents_created_total[5m])

# Scheduler queue length
aether_scheduler_queue_length
```

### Logging

**Structured Logging:**
All logs are in JSON format for easy parsing:

```json
{
  "timestamp": "2026-02-09T10:00:00Z",
  "level": "info",
  "component": "api_server",
  "message": "agent created",
  "agent_id": "agent-abc123",
  "tenant_id": "tenant-123",
  "request_id": "req-xyz789"
}
```

**Log Aggregation:**
- AWS: CloudWatch Logs Insights
- GCP: Cloud Logging
- Self-hosted: ELK Stack or Loki

**Useful Log Queries:**

```
# All errors in last hour
fields @timestamp, component, message, error
| filter level = "error"
| sort @timestamp desc

# Agent creation failures
fields @timestamp, agent_id, error
| filter component = "runtime" and message = "agent creation failed"

# Slow API requests
fields @timestamp, path, duration_ms
| filter duration_ms > 1000
| sort duration_ms desc
```

### Alerts

**Critical Alerts (PagerDuty/Ops Genie):**
- Service down (health check failing)
- Database connection lost
- High error rate (> 5%)
- Out of memory errors

**Warning Alerts (Email/Slack):**
- High CPU usage (> 80%)
- Approaching quota limits (> 80%)
- Slow API requests (p99 > 1s)
- Failed agent starts (> 5%)

---

## Backup & Recovery

Aether runs single-region. Durability relies on the managed data services rather than
an application-level backup subsystem.

### Backup Strategy

- **PostgreSQL:** Enable managed automated backups and snapshots (RDS/Cloud SQL). Set a
  retention window (for example 30 days) and enable point-in-time recovery.
- **Redis:** Enable managed snapshots (ElastiCache/Memorystore) for cache and session state.
- **Configuration:** Keep config files and Terraform state in version control and object
  storage (S3/GCS).

**Example (AWS RDS automated backups):**
```bash
aws rds modify-db-instance \
  --db-instance-identifier "aether-db" \
  --backup-retention-period 30 \
  --apply-immediately
```

### RTO & RPO

**Recovery Time Objective (RTO):** < 30 minutes (restore from latest snapshot)
**Recovery Point Objective (RPO):** determined by the managed backup interval

**Recovery Procedure:**

1. **Detect failure** (automatic via health checks)
2. **Restore the database** from the latest managed snapshot
3. **Redeploy the API tier** (Terraform / ASG)
4. **Verify services** (automated smoke tests)
5. **Update DNS** if the load balancer endpoint changed

---

## Scaling

### Horizontal Scaling

**Auto-scaling Rules:**

```yaml
autoscaling:
  compute_nodes:
    min: 5
    max: 50
    metrics:
      - type: "cpu"
        target: 70
      - type: "memory"
        target: 80
      - type: "custom"
        name: "agent_queue_length"
        target: 10
```

**Manual Scaling:**
```bash
# Scale up compute nodes
aws autoscaling set-desired-capacity \
  --auto-scaling-group-name "aether-compute-nodes" \
  --desired-capacity 20

# Scale up control plane (requires updating ASG config)
terraform apply -var="control_plane_count=5"
```

### Vertical Scaling

**Database:**
```bash
# Increase database instance size
aws rds modify-db-instance \
  --db-instance-identifier "aether-db" \
  --db-instance-class "db.r5.4xlarge" \
  --apply-immediately
```

**Application:**
```bash
# Update instance type in Terraform
terraform apply -var="control_plane_instance_type=c5.9xlarge"
```

### Performance Tuning

See [Performance Tuning Guide](./PERFORMANCE_TUNING.md)

---

## Troubleshooting

### Common Issues

**Service won't start:**
```bash
# Check logs
journalctl -u aether -n 100 --no-pager

# Check configuration
aether config validate

# Check dependencies
aether health check
```

**Database connection errors:**
```bash
# Test connection
psql "$DATABASE_URL"

# Check security groups
aws ec2 describe-security-groups --group-ids sg-abc123

# Check network
nc -zv aether-db.abc123.rds.amazonaws.com 5432
```

**High memory usage:**
```bash
# Check memory usage by component
ps aux --sort=-%mem | head -20

# Check for memory leaks
curl localhost:9090/metrics | grep go_memstats

# Restart service
sudo systemctl restart aether
```

**Agents not starting:**
```bash
# Check scheduler logs
journalctl -u aether -f | grep scheduler

# Check compute node status
aether nodes list

# Check resource availability
aether quotas status
```

### Debug Mode

Enable debug logging:
```bash
# Temporary (current session)
export LOG_LEVEL=debug
sudo systemctl restart aether

# Permanent
sudo vim /etc/aether/config.yaml
# Change: logging.level: "debug"
sudo systemctl restart aether
```

### Support

- **Documentation:** https://docs.aether.example.com
- **Community:** https://community.aether.example.com
- **Enterprise Support:** support@aether.example.com
- **Emergency Hotline:** +1-xxx-xxx-xxxx (24/7)

---

## Next Steps

- [ ] Set up monitoring dashboards
- [ ] Configure alerting
- [ ] Run load tests
- [ ] Document runbooks
- [ ] Train operations team
- [ ] Schedule DR drills
- [ ] Set up cost monitoring
- [ ] Review security audit

---

**Last Updated:** 2026-02-09
