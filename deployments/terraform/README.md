# Aether Multi-Cloud Terraform Modules

Infrastructure as Code for deploying Aether across AWS, GCP, Azure, and on-premise environments.

## Overview

This directory contains Terraform modules for deploying Aether's infrastructure across multiple cloud providers:

- **AWS** - VPC, EKS (optional), RDS PostgreSQL, ElastiCache Redis, MSK Kafka
- **GCP** - VPC, GKE (optional), Cloud SQL, Memorystore Redis, Pub/Sub
- **Azure** - VNet, AKS (optional), Azure Database, Azure Cache, Event Hubs
- **On-Premise** - Networking, storage, compute resources

## Quick Start

### AWS Deployment

```bash
cd aws

# Initialize Terraform
terraform init

# Create terraform.tfvars
cat > terraform.tfvars <<EOF
cluster_name        = "aether-prod"
region              = "us-east-1"
environment         = "prod"
postgres_password   = "change-me"
redis_auth_token    = "change-me"
EOF

# Plan
terraform plan

# Apply
terraform apply
```

### GCP Deployment

```bash
cd gcp

# Initialize Terraform
terraform init

# Create terraform.tfvars
cat > terraform.tfvars <<EOF
project_id          = "my-project-id"
cluster_name        = "aether-prod"
region              = "us-central1"
environment         = "prod"
postgres_password   = "change-me"
EOF

# Plan
terraform plan

# Apply
terraform apply
```

## Architecture

### AWS Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         AWS VPC                              │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐    │
│  │ Public AZ-A  │  │ Public AZ-B  │  │ Public AZ-C  │    │
│  │   10.0.1.0   │  │   10.0.2.0   │  │   10.0.3.0   │    │
│  └──────────────┘  └──────────────┘  └──────────────┘    │
│         │                  │                  │            │
│    ┌────┴───────────────────┴─────────────────┴─────┐     │
│    │          Internet Gateway                       │     │
│    └─────────────────────────────────────────────────┘     │
│                                                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐    │
│  │ Private AZ-A │  │ Private AZ-B │  │ Private AZ-C │    │
│  │  10.0.11.0   │  │  10.0.12.0   │  │  10.0.13.0   │    │
│  │              │  │              │  │              │    │
│  │  ┌────────┐  │  │  ┌────────┐  │  │  ┌────────┐  │    │
│  │  │  RDS   │  │  │  │  MSK   │  │  │  │ Redis  │  │    │
│  │  │Postgres│  │  │  │ Kafka  │  │  │  │ElastiCache  │    │
│  │  └────────┘  │  │  └────────┘  │  │  └────────┘  │    │
│  └──────────────┘  └──────────────┘  └──────────────┘    │
│                                                             │
└─────────────────────────────────────────────────────────────┘
          │
          └─► S3 Buckets (Backups)
          └─► Secrets Manager (Credentials)
          └─► CloudWatch (Logs & Metrics)
```

### GCP Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     GCP VPC Network                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────────────────┐  ┌──────────────────────┐       │
│  │   Public Subnet      │  │   Private Subnet     │       │
│  │     10.0.1.0/24      │  │     10.0.2.0/24      │       │
│  │                      │  │                      │       │
│  │  ┌────────────────┐  │  │  ┌─────────────┐    │       │
│  │  │  Cloud NAT     │  │  │  │  Cloud SQL  │    │       │
│  │  │  (egress)      │  │  │  │  Postgres   │    │       │
│  │  └────────────────┘  │  │  └─────────────┘    │       │
│  │                      │  │                      │       │
│  └──────────────────────┘  │  ┌─────────────┐    │       │
│                            │  │ Memorystore │    │       │
│                            │  │   Redis     │    │       │
│                            │  └─────────────┘    │       │
│                            └──────────────────────┘       │
│                                                             │
└─────────────────────────────────────────────────────────────┘
          │
          └─► Cloud Storage (Backups)
          └─► Secret Manager (Credentials)
          └─► Cloud Logging (Logs & Metrics)
```

## Cost Optimization

### Development Environment

For development, use these cost-optimized settings:

**AWS:**
```hcl
environment             = "dev"
postgres_instance_class = "db.t3.micro"
postgres_multi_az       = false
redis_node_type         = "cache.t3.micro"
redis_num_nodes         = 1
kafka_instance_type     = "kafka.t3.small"
kafka_num_brokers       = 1
single_nat_gateway      = true
```

**GCP:**
```hcl
environment       = "dev"
postgres_tier     = "db-f1-micro"
postgres_ha       = false
redis_tier        = "BASIC"
redis_memory_gb   = 1
```

### Production Environment

For production, use these highly-available settings:

**AWS:**
```hcl
environment             = "prod"
postgres_instance_class = "db.r6g.xlarge"
postgres_multi_az       = true
redis_node_type         = "cache.r6g.large"
redis_num_nodes         = 3
kafka_instance_type     = "kafka.m5.large"
kafka_num_brokers       = 3
single_nat_gateway      = false
```

**GCP:**
```hcl
environment       = "prod"
postgres_tier     = "db-custom-4-15360"
postgres_ha       = true
redis_tier        = "STANDARD_HA"
redis_memory_gb   = 10
```

## Security Best Practices

### Secrets Management

Never commit sensitive values to version control. Use one of these approaches:

**Option 1: Environment Variables**
```bash
export TF_VAR_postgres_password="secure-password"
export TF_VAR_redis_auth_token="secure-token"
terraform apply
```

**Option 2: terraform.tfvars (gitignored)**
```hcl
# terraform.tfvars (add to .gitignore)
postgres_password = "secure-password"
redis_auth_token  = "secure-token"
```

**Option 3: Secrets Manager/Vault**
```bash
# Fetch from AWS Secrets Manager
export TF_VAR_postgres_password=$(aws secretsmanager get-secret-value \
  --secret-id aether/postgres/password --query SecretString --output text)
```

### Network Security

- All databases are in private subnets (no public IPs)
- Security groups restrict access to VPC CIDR only
- Encryption at rest enabled for all storage
- Encryption in transit (TLS) enabled for all connections
- Network ACLs provide additional subnet-level protection

### IAM/RBAC

Follow principle of least privilege:
- Create dedicated service accounts for Aether
- Use IAM roles (not access keys) for AWS resources
- Use Workload Identity for GCP resources
- Rotate credentials regularly
- Enable MFA for admin access

## Multi-Region Deployment

### Active-Passive Setup

Deploy primary in us-east-1, secondary in us-west-2:

```bash
# Primary region
cd aws
terraform workspace new prod-primary
terraform apply -var="region=us-east-1"

# Secondary region (for DR)
terraform workspace new prod-secondary
terraform apply -var="region=us-west-2"
```

### Cross-Region Replication

Enable replication for disaster recovery:

**PostgreSQL:** Use RDS read replicas
```hcl
resource "aws_db_instance" "replica" {
  replicate_source_db = aws_db_instance.aether.arn
  instance_class      = var.postgres_instance_class
  region              = "us-west-2"
}
```

**S3:** Enable cross-region replication
```hcl
resource "aws_s3_bucket_replication_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id

  rule {
    id     = "replicate-to-dr"
    status = "Enabled"

    destination {
      bucket        = aws_s3_bucket.backups_dr.arn
      storage_class = "STANDARD_IA"
    }
  }
}
```

## Monitoring and Alerting

### CloudWatch (AWS)

Key metrics to monitor:
- RDS: `DatabaseConnections`, `CPUUtilization`, `FreeStorageSpace`
- ElastiCache: `CurrConnections`, `CPUUtilization`, `BytesUsedForCache`
- MSK: `BytesInPerSec`, `BytesOutPerSec`, `UnderReplicatedPartitions`

### Cloud Monitoring (GCP)

Key metrics to monitor:
- Cloud SQL: `database/cpu/utilization`, `database/disk/utilization`
- Memorystore: `redis/stats/connections/total`, `redis/stats/memory/usage_ratio`

## Backup and Restore

### Automated Backups

All databases have automated backups enabled:
- **RDS**: Daily snapshots, 30-day retention, 7-day transaction logs
- **Cloud SQL**: Daily backups, point-in-time recovery enabled
- **Redis**: Daily RDB snapshots
- **S3/Cloud Storage**: Versioning enabled, lifecycle policies

### Manual Backup

```bash
# AWS RDS
aws rds create-db-snapshot \
  --db-instance-identifier aether-postgres \
  --db-snapshot-identifier aether-manual-$(date +%Y%m%d)

# GCP Cloud SQL
gcloud sql backups create \
  --instance=aether-postgres \
  --description="Manual backup $(date)"
```

### Restore from Backup

```bash
# AWS RDS
aws rds restore-db-instance-from-db-snapshot \
  --db-instance-identifier aether-postgres-restored \
  --db-snapshot-identifier aether-manual-20240101

# GCP Cloud SQL
gcloud sql backups restore BACKUP_ID \
  --backup-instance=aether-postgres \
  --backup-instance=aether-postgres
```

## Troubleshooting

### Terraform State Lock

If terraform gets stuck with "acquiring state lock":

```bash
# AWS (DynamoDB lock)
terraform force-unlock LOCK_ID

# GCP (Cloud Storage lock)
gsutil rm gs://my-terraform-state/.terraform.tfstate.lock.info
```

### Connection Issues

Verify security group/firewall rules:

```bash
# AWS
aws ec2 describe-security-groups --group-ids sg-xxxxx

# GCP
gcloud compute firewall-rules list --filter="network=aether-vpc"
```

### Cost Overruns

Check resource utilization:

```bash
# AWS Cost Explorer
aws ce get-cost-and-usage \
  --time-period Start=2024-01-01,End=2024-01-31 \
  --granularity MONTHLY \
  --metrics BlendedCost

# GCP Billing
gcloud billing accounts list
gcloud alpha billing accounts projects describe PROJECT_ID
```

## Cleanup

To destroy all resources:

```bash
# DANGER: This will delete ALL infrastructure
terraform destroy

# To preserve backups, exclude them:
terraform destroy -target=module.vpc
terraform destroy -target=aws_db_instance.aether
# etc. (but keep S3 bucket)
```

## Module Structure

```
terraform/
├── aws/                    # AWS-specific resources
│   ├── main.tf            # Main AWS configuration
│   ├── variables.tf       # Input variables
│   └── outputs.tf         # Output values
├── gcp/                    # GCP-specific resources
│   ├── main.tf
│   ├── variables.tf
│   └── outputs.tf
├── azure/                  # Azure-specific resources (placeholder)
├── on-premise/             # On-premise deployment (placeholder)
└── modules/                # Reusable modules
    ├── networking/         # VPC/VNet module
    └── kubernetes/         # K8s cluster module (placeholder)
```

## Contributing

When adding new infrastructure:
1. Add variables to `variables.tf`
2. Add resources to `main.tf`
3. Add outputs to `outputs.tf`
4. Update this README with usage examples
5. Test in dev environment first
6. Document any manual steps required

## References

- [AWS Provider Documentation](https://registry.terraform.io/providers/hashicorp/aws/latest/docs)
- [GCP Provider Documentation](https://registry.terraform.io/providers/hashicorp/google/latest/docs)
- [Terraform Best Practices](https://www.terraform.io/docs/cloud/guides/recommended-practices/index.html)
- [Aether Architecture Guide](../../docs/architecture/system-overview.md)
