# Aether Runtime - AWS Terraform Module

Terraform module for deploying Aether Runtime on AWS with production-ready infrastructure.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         VPC (10.0.0.0/16)                   │
│                                                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │ Public Subnet│  │ Public Subnet│  │ Public Subnet│     │
│  │  (AZ-1)      │  │  (AZ-2)      │  │  (AZ-3)      │     │
│  │              │  │              │  │              │     │
│  │  ┌────────┐  │  │  ┌────────┐  │  │  ┌────────┐  │     │
│  │  │  NAT   │  │  │  │  NAT   │  │  │  │  NAT   │  │     │
│  │  │Gateway │  │  │  │Gateway │  │  │  │Gateway │  │     │
│  │  └────────┘  │  │  └────────┘  │  │  └────────┘  │     │
│  │      │       │  │      │       │  │      │       │     │
│  │  ┌────────┐  │  │  ┌────────┐  │  │  ┌────────┐  │     │
│  │  │  ALB   │  │  │  │  ALB   │  │  │  │  ALB   │  │     │
│  │  └────────┘  │  │  └────────┘  │  │  └────────┘  │     │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘     │
│         │                 │                 │             │
│  ┌──────▼───────┐  ┌──────▼───────┐  ┌──────▼───────┐     │
│  │Private Subnet│  │Private Subnet│  │Private Subnet│     │
│  │  (AZ-1)      │  │  (AZ-2)      │  │  (AZ-3)      │     │
│  │              │  │              │  │              │     │
│  │  ┌────────┐  │  │  ┌────────┐  │  │  ┌────────┐  │     │
│  │  │  ECS   │  │  │  │  ECS   │  │  │  │  ECS   │  │     │
│  │  │ Tasks  │  │  │  │ Tasks  │  │  │  │ Tasks  │  │     │
│  │  └────────┘  │  │  └────────┘  │  │  └────────┘  │     │
│  │      │       │  │      │       │  │      │       │     │
│  │  ┌────────┐  │  │  ┌────────┐  │  │              │     │
│  │  │  RDS   │  │  │  │  RDS   │  │  │              │     │
│  │  │Primary │◄─┼──┼─►│Standby │  │  │              │     │
│  │  └────────┘  │  │  └────────┘  │  │              │     │
│  │              │  │              │  │              │     │
│  │  ┌────────┐  │  │  ┌────────┐  │  │  ┌────────┐  │     │
│  │  │ Redis  │  │  │  │ Redis  │  │  │  │ Redis  │  │     │
│  │  │ Primary│◄─┼──┼─►│Replica │◄─┼──┼─►│Replica │  │     │
│  │  └────────┘  │  │  └────────┘  │  │  └────────┘  │     │
│  └──────────────┘  └──────────────┘  └──────────────┘     │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Components

### Networking
- **VPC**: Isolated network with configurable CIDR block
- **Subnets**: Public and private subnets across multiple AZs
- **NAT Gateway**: Enables private subnet internet access
- **Internet Gateway**: Public subnet internet connectivity
- **Route Tables**: Custom routing for traffic flow
- **VPC Flow Logs**: Network traffic monitoring

### Compute
- **ECS Cluster**: Fargate-based container orchestration
- **ECS Services**: Auto-scaling API server and runtime
- **Application Load Balancer**: Traffic distribution and health checks
- **Target Groups**: ALB routing configuration
- **CloudWatch Logs**: Centralized logging

### Data Layer
- **RDS PostgreSQL**: Managed relational database
  - Multi-AZ deployment for HA
  - Automated backups
  - Performance Insights
  - Encryption at rest
- **ElastiCache Redis**: In-memory caching and session storage
  - Multi-AZ replication
  - Automatic failover
  - Snapshot backups

### Security
- **Security Groups**: Granular network access control
- **Secrets Manager**: Credential management
- **IAM Roles**: Least-privilege access
- **Encryption**: At-rest and in-transit

## Prerequisites

- Terraform >= 1.0
- AWS CLI configured with appropriate credentials
- AWS account with sufficient permissions

## Quick Start

### 1. Initialize Terraform

```bash
cd terraform/aws
terraform init
```

### 2. Configure Variables

Create a `terraform.tfvars` file:

```hcl
aws_region   = "us-east-1"
environment  = "prod"
project_name = "aether"

# Network
vpc_cidr    = "10.0.0.0/16"
az_count    = 3

# Database
db_instance_class = "db.t3.medium"
db_multi_az       = true

# Redis
redis_node_type = "cache.t3.micro"
redis_num_cache_nodes = 2

# ECS
container_image   = "ghcr.io/aether-runtime/aether:v0.2.0"
ecs_desired_count = 2
ecs_min_capacity  = 1
ecs_max_capacity  = 10

# Tags
common_tags = {
  Team    = "platform"
  Owner   = "ops@example.com"
  Project = "aether"
}
```

### 3. Plan Deployment

```bash
terraform plan -out=tfplan
```

### 4. Apply Infrastructure

```bash
terraform apply tfplan
```

### 5. Get Outputs

```bash
terraform output
```

## Configuration

### Environment Profiles

**Development:**
```hcl
environment          = "dev"
db_instance_class    = "db.t3.small"
db_multi_az          = false
redis_num_cache_nodes = 1
single_nat_gateway   = true  # Cost optimization
ecs_desired_count    = 1
```

**Staging:**
```hcl
environment          = "staging"
db_instance_class    = "db.t3.medium"
db_multi_az          = true
redis_num_cache_nodes = 2
single_nat_gateway   = false
ecs_desired_count    = 2
```

**Production:**
```hcl
environment          = "prod"
db_instance_class    = "db.r6g.large"
db_multi_az          = true
redis_num_cache_nodes = 3
redis_automatic_failover_enabled = true
single_nat_gateway   = false
ecs_desired_count    = 3
ecs_min_capacity     = 2
ecs_max_capacity     = 20
```

### Backend Configuration

For production, configure S3 backend for state storage:

```hcl
# backend.tf
terraform {
  backend "s3" {
    bucket         = "aether-terraform-state"
    key            = "prod/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "aether-terraform-locks"
  }
}
```

Create the S3 bucket and DynamoDB table:

```bash
# Create state bucket
aws s3 mb s3://aether-terraform-state --region us-east-1
aws s3api put-bucket-versioning \
  --bucket aether-terraform-state \
  --versioning-configuration Status=Enabled

# Create lock table
aws dynamodb create-table \
  --table-name aether-terraform-locks \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST \
  --region us-east-1
```

## Outputs

### Important Outputs

- `connection_info`: Complete connection details
- `alb_dns_name`: API endpoint URL
- `rds_endpoint`: Database connection string
- `redis_endpoint`: Redis connection string
- `deployment_commands`: Useful AWS CLI commands

### Accessing Secrets

Retrieve database password:

```bash
aws secretsmanager get-secret-value \
  --secret-id $(terraform output -raw rds_master_user_secret_arn) \
  --query SecretString --output text | jq -r .password
```

## Operations

### Scaling ECS Services

```bash
# Manual scaling
aws ecs update-service \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --service $(terraform output -raw ecs_service_name) \
  --desired-count 5

# Auto-scaling is configured automatically based on CPU/memory
```

### Viewing Logs

```bash
# Tail logs
aws logs tail $(terraform output -raw log_group_name) --follow

# Filter logs
aws logs tail $(terraform output -raw log_group_name) \
  --filter-pattern "ERROR" --follow
```

### ECS Exec (Debugging)

```bash
# List tasks
aws ecs list-tasks \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --service $(terraform output -raw ecs_service_name)

# Connect to task
aws ecs execute-command \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --task <TASK_ID> \
  --container aether \
  --interactive \
  --command "/bin/sh"
```

### Database Maintenance

```bash
# Create manual snapshot
aws rds create-db-snapshot \
  --db-instance-identifier aether-prod-rds \
  --db-snapshot-identifier aether-manual-backup-$(date +%Y%m%d)

# List snapshots
aws rds describe-db-snapshots \
  --db-instance-identifier aether-prod-rds
```

## Cost Optimization

### Development Environment

- Use `single_nat_gateway = true` (saves ~$32/month per AZ)
- Use `db.t3.small` instance ($24/month vs $61/month for t3.medium)
- Use `redis_num_cache_nodes = 1` (single node)
- Set `db_multi_az = false`

**Estimated Monthly Cost (Dev):** ~$150-200

### Production Environment

- Use multi-AZ for HA
- Use reserved instances for cost savings (up to 72%)
- Enable S3 lifecycle policies for old backups
- Use auto-scaling to match demand

**Estimated Monthly Cost (Prod, moderate traffic):** ~$500-800

## Security Best Practices

1. **Enable encryption everywhere**
   - RDS encryption at rest
   - ElastiCache encryption in transit
   - S3 bucket encryption for backups

2. **Use Secrets Manager for credentials**
   - Never hardcode passwords
   - Rotate secrets regularly

3. **Implement least-privilege IAM**
   - Separate roles for services
   - Avoid wildcard permissions

4. **Enable monitoring**
   - VPC Flow Logs
   - CloudWatch Logs
   - AWS GuardDuty
   - AWS Security Hub

5. **Network isolation**
   - Database in private subnets
   - Security group restrictions
   - NACLs for additional layer

## Troubleshooting

### ECS Tasks Not Starting

```bash
# Check task status
aws ecs describe-tasks \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --tasks <TASK_ARN>

# Check service events
aws ecs describe-services \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --services $(terraform output -raw ecs_service_name) \
  --query 'services[0].events' --output table
```

### Database Connection Issues

```bash
# Test connectivity from ECS task
aws ecs execute-command \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --task <TASK_ID> \
  --container aether \
  --interactive \
  --command "nc -zv $(terraform output -raw rds_endpoint) 5432"
```

### Terraform State Lock

```bash
# If state is locked
terraform force-unlock <LOCK_ID>

# Verify DynamoDB table
aws dynamodb scan --table-name aether-terraform-locks
```

## Cleanup

To destroy all infrastructure:

```bash
# Review what will be destroyed
terraform plan -destroy

# Destroy infrastructure
terraform destroy

# Confirm by typing 'yes'
```

**Warning:** This will delete all data. Take backups before destroying production infrastructure.

## Module Structure

```
terraform/aws/
├── main.tf              # Root module
├── variables.tf         # Input variables
├── outputs.tf           # Output values
├── README.md            # This file
├── modules/
│   ├── vpc/             # VPC and networking
│   ├── ecs/             # ECS cluster and services
│   ├── rds/             # RDS PostgreSQL
│   └── elasticache/     # ElastiCache Redis
└── examples/
    └── complete/        # Complete example configuration
```

## Support

- [Terraform AWS Provider Documentation](https://registry.terraform.io/providers/hashicorp/aws/latest/docs)
- [Aether Runtime Documentation](../../docs/)
- [GitHub Issues](https://github.com/aether-runtime/aether/issues)

## License

MIT License - see LICENSE file for details
