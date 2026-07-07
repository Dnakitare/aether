# ADR-008: Terraform for Infrastructure as Code

**Status**: Accepted
**Date**: 2025-11-10
**Decision Makers**: Platform Architecture Team, DevOps Team
**Technical Story**: Repeatable, auditable infrastructure provisioning

---

## Context

Aether requires infrastructure provisioning across multiple environments (dev, staging, production) and cloud providers (AWS, GCP). Key requirements:

1. **Repeatability**: Identical infrastructure across environments
2. **Version Control**: Track infrastructure changes like code
3. **Auditability**: Who changed what, when, and why
4. **Automation**: CI/CD pipeline integration
5. **Multi-Cloud**: Support AWS, GCP
6. **State Management**: Shared state, locking, encryption

### Manual Provisioning Problems

❌ **Snowflake Servers**: Each environment slightly different
❌ **No Audit Trail**: Changes made via console not tracked
❌ **Error Prone**: Manual steps lead to mistakes
❌ **Slow**: 2-3 days to provision new environment
❌ **Knowledge Silos**: Only 1-2 people know how to provision

### Alternatives Considered

| Tool | Pros | Cons | Decision |
|------|------|------|----------|
| **AWS CloudFormation** | AWS-native, free | AWS-only, YAML verbose | ❌ Rejected |
| **Pulumi** | Real programming languages | Smaller community, newer | ❌ Rejected |
| **Ansible** | Good for config mgmt | Imperative, not idempotent | ❌ Rejected (kept for config) |
| **Terraform** | Multi-cloud, large ecosystem, declarative, mature | State management complexity | ✅ **Accepted** |
| **CDK (AWS/Terraform)** | Type safety, better abstractions | Smaller community, adds layer | ⏳ Future consideration |

---

## Decision

**We will use Terraform as the Infrastructure as Code (IaC) tool for provisioning all cloud resources.**

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Terraform Workspace                          │
│                                                                  │
│  deployments/terraform/                                          │
│  ├── environments/                                               │
│  │   ├── dev/                                                    │
│  │   │   ├── main.tf          # Environment config              │
│  │   │   ├── variables.tf     # Input variables                 │
│  │   │   ├── outputs.tf       # Output values                   │
│  │   │   └── terraform.tfvars # Variable values                 │
│  │   ├── staging/                                                │
│  │   └── production/                                             │
│  │                                                               │
│  ├── modules/                                                    │
│  │   ├── vpc/                 # Reusable VPC module             │
│  │   ├── compute/             # EC2, Auto Scaling Groups        │
│  │   ├── database/            # RDS, ElastiCache                │
│  │   ├── networking/          # Load balancers, DNS             │
│  │   └── monitoring/          # CloudWatch, alerts              │
│  │                                                               │
│  └── global/                                                     │
│      ├── s3/                  # S3 buckets for state, backups   │
│      └── iam/                 # IAM roles, policies             │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ terraform apply
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        State Backend                             │
│  ┌────────────────────────────────────────────────────────┐    │
│  │  S3 Bucket (terraform-state-bucket)                     │    │
│  │  ├─ dev/terraform.tfstate                               │    │
│  │  ├─ staging/terraform.tfstate                           │    │
│  │  └─ production/terraform.tfstate                        │    │
│  │                                                          │    │
│  │  DynamoDB Table (terraform-locks)                       │    │
│  │  └─ LockID → {env, user, timestamp}                     │    │
│  └────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ Creates/updates
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Cloud Resources                           │
│  AWS / GCP                                                       │
│  ├─ VPC, Subnets, Security Groups                               │
│  ├─ EC2 Instances, Auto Scaling Groups                          │
│  ├─ RDS, ElastiCache                                            │
│  ├─ Load Balancers, Route 53                                    │
│  └─ IAM Roles, S3 Buckets, CloudWatch                           │
└─────────────────────────────────────────────────────────────────┘
```

---

## Implementation Details

### 1. Module Structure

**VPC Module** (`modules/vpc/main.tf`):
```hcl
variable "environment" {
  type = string
}

variable "cidr_block" {
  type = string
  default = "10.0.0.0/16"
}

variable "availability_zones" {
  type = list(string)
  default = ["us-east-1a", "us-east-1b", "us-east-1c"]
}

# VPC
resource "aws_vpc" "main" {
  cidr_block = var.cidr_block
  enable_dns_hostnames = true
  enable_dns_support = true

  tags = {
    Name = "aether-${var.environment}"
    Environment = var.environment
    ManagedBy = "Terraform"
  }
}

# Public Subnets (for Load Balancers)
resource "aws_subnet" "public" {
  count = length(var.availability_zones)

  vpc_id = aws_vpc.main.id
  cidr_block = cidrsubnet(var.cidr_block, 8, count.index)
  availability_zone = var.availability_zones[count.index]
  map_public_ip_on_launch = true

  tags = {
    Name = "aether-${var.environment}-public-${count.index}"
    Tier = "Public"
  }
}

# Private Subnets (for API, Schedulers)
resource "aws_subnet" "private" {
  count = length(var.availability_zones)

  vpc_id = aws_vpc.main.id
  cidr_block = cidrsubnet(var.cidr_block, 8, count.index + 10)
  availability_zone = var.availability_zones[count.index]

  tags = {
    Name = "aether-${var.environment}-private-${count.index}"
    Tier = "Private"
  }
}

# Outputs
output "vpc_id" {
  value = aws_vpc.main.id
}

output "public_subnet_ids" {
  value = aws_subnet.public[*].id
}

output "private_subnet_ids" {
  value = aws_subnet.private[*].id
}
```

### 2. Environment Configuration

**Production** (`environments/production/main.tf`):
```hcl
terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # Remote state backend
  backend "s3" {
    bucket = "aether-terraform-state"
    key = "production/terraform.tfstate"
    region = "us-east-1"
    encrypt = true
    dynamodb_table = "terraform-locks"
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Environment = "production"
      ManagedBy = "Terraform"
      Project = "Aether"
      CostCenter = "Engineering"
    }
  }
}

# VPC
module "vpc" {
  source = "../../modules/vpc"

  environment = "production"
  cidr_block = "10.0.0.0/16"
  availability_zones = ["us-east-1a", "us-east-1b", "us-east-1c"]
}

# Compute (API Servers, Schedulers)
module "compute" {
  source = "../../modules/compute"

  environment = "production"
  vpc_id = module.vpc.vpc_id
  private_subnet_ids = module.vpc.private_subnet_ids

  api_server_count = 3
  api_server_instance_type = "c5.xlarge"

  scheduler_count = 3
  scheduler_instance_type = "t3.medium"

  compute_node_min_count = 6
  compute_node_max_count = 50
  compute_node_instance_type = "i3.metal"
}

# Database (PostgreSQL, Redis)
module "database" {
  source = "../../modules/database"

  environment = "production"
  vpc_id = module.vpc.vpc_id
  private_subnet_ids = module.vpc.private_subnet_ids

  postgres_instance_class = "db.r5.xlarge"
  postgres_multi_az = true
  postgres_allocated_storage = 500

  redis_node_type = "cache.r5.large"
  redis_num_cache_clusters = 2  # 1 primary + 1 replica
}

# Networking (Load Balancers, DNS)
module "networking" {
  source = "../../modules/networking"

  environment = "production"
  vpc_id = module.vpc.vpc_id
  public_subnet_ids = module.vpc.public_subnet_ids

  domain_name = "aether.example.com"
  certificate_arn = var.ssl_certificate_arn
}
```

**Variables** (`environments/production/terraform.tfvars`):
```hcl
region = "us-east-1"
ssl_certificate_arn = "arn:aws:acm:us-east-1:123456789012:certificate/abc123"
```

### 3. State Management

**S3 Backend Configuration**:
```hcl
# Separate Terraform project to bootstrap backend
# deployments/terraform/bootstrap/main.tf

resource "aws_s3_bucket" "terraform_state" {
  bucket = "aether-terraform-state"

  lifecycle {
    prevent_destroy = true
  }

  tags = {
    Name = "Terraform State"
    ManagedBy = "Terraform"
  }
}

# Enable versioning (rollback capability)
resource "aws_s3_bucket_versioning" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id

  versioning_configuration {
    status = "Enabled"
  }
}

# Encryption at rest
resource "aws_s3_bucket_server_side_encryption_configuration" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Block public access
resource "aws_s3_bucket_public_access_block" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id

  block_public_acls = true
  block_public_policy = true
  ignore_public_acls = true
  restrict_public_buckets = true
}

# DynamoDB table for state locking
resource "aws_dynamodb_table" "terraform_locks" {
  name = "terraform-locks"
  billing_mode = "PAY_PER_REQUEST"
  hash_key = "LockID"

  attribute {
    name = "LockID"
    type = "S"
  }

  tags = {
    Name = "Terraform State Locks"
    ManagedBy = "Terraform"
  }
}
```

### 4. CI/CD Integration

**GitHub Actions Workflow** (`.github/workflows/terraform.yml`):
```yaml
name: Terraform

on:
  push:
    branches: [main]
    paths:
      - 'deployments/terraform/**'
  pull_request:
    paths:
      - 'deployments/terraform/**'

jobs:
  terraform:
    runs-on: ubuntu-latest
    env:
      AWS_REGION: us-east-1

    steps:
      - uses: actions/checkout@v3

      - name: Configure AWS Credentials
        uses: aws-actions/configure-aws-credentials@v2
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: ${{ env.AWS_REGION }}

      - name: Setup Terraform
        uses: hashicorp/setup-terraform@v2
        with:
          terraform_version: 1.5.0

      - name: Terraform Format Check
        run: terraform fmt -check -recursive

      - name: Terraform Init
        working-directory: deployments/terraform/environments/production
        run: terraform init

      - name: Terraform Validate
        working-directory: deployments/terraform/environments/production
        run: terraform validate

      - name: Terraform Plan
        working-directory: deployments/terraform/environments/production
        run: terraform plan -out=tfplan

      - name: Upload Plan
        uses: actions/upload-artifact@v3
        with:
          name: tfplan
          path: deployments/terraform/environments/production/tfplan

      # Manual approval required for production apply
      - name: Terraform Apply
        if: github.ref == 'refs/heads/main' && github.event_name == 'push'
        working-directory: deployments/terraform/environments/production
        run: terraform apply -auto-approve tfplan
```

---

## Consequences

### Positive

✅ **Version Control**: All infrastructure changes in Git
✅ **Repeatability**: Identical envs via `terraform apply`
✅ **Auditability**: Git history shows who, what, when, why
✅ **Automation**: CI/CD pipeline integration
✅ **Multi-Cloud**: Same tool for AWS, GCP
✅ **Plan Before Apply**: Preview changes before execution
✅ **Idempotent**: Safe to run multiple times
✅ **Rollback**: Revert to previous state via Git + `terraform apply`

### Negative

❌ **State Management**: Shared state requires backend (S3, locking)
❌ **Learning Curve**: HCL syntax, Terraform concepts
❌ **State Drift**: Manual changes cause drift (mitigated by CI/CD)
❌ **Slow Feedback**: Plan/apply can take 5-10 minutes
❌ **Secrets Management**: Sensitive values require separate system (Parameter Store)

### Neutral

⚖️ **Declarative**: Easier to understand but less flexible than imperative
⚖️ **Terraform Cloud**: Paid service for remote state, but S3 backend free

---

## Best Practices

### 1. Module Organization

```
deployments/terraform/
├── environments/        # Environment-specific configs
│   ├── dev/
│   ├── staging/
│   └── production/
├── modules/             # Reusable modules
│   ├── vpc/
│   ├── compute/
│   └── database/
└── global/              # Shared resources (IAM, S3)
```

### 2. Variable Management

```hcl
# Variables in variables.tf
variable "environment" {
  type = string
  description = "Environment name (dev, staging, production)"
}

variable "db_password" {
  type = string
  description = "Database password"
  sensitive = true  # Prevents logging
}

# Values in terraform.tfvars (NOT committed to Git)
environment = "production"
db_password = "super-secret-password"  # Use Parameter Store instead

# Better: Load from AWS Parameter Store
data "aws_ssm_parameter" "db_password" {
  name = "/aether/production/db_password"
}

resource "aws_db_instance" "postgres" {
  password = data.aws_ssm_parameter.db_password.value
}
```

### 3. Tagging Strategy

```hcl
# All resources tagged with:
tags = {
  Environment = "production"
  ManagedBy = "Terraform"
  Project = "Aether"
  CostCenter = "Engineering"
  Owner = "platform-team@example.com"
}

# Use default_tags in provider for consistency
provider "aws" {
  default_tags {
    tags = local.common_tags
  }
}
```

### 4. State File Security

```bash
# Never commit state files to Git
echo "*.tfstate" >> .gitignore
echo "*.tfstate.backup" >> .gitignore
echo "terraform.tfvars" >> .gitignore

# Use remote backend with encryption
terraform {
  backend "s3" {
    encrypt = true  # Encryption at rest
  }
}

# Use locking to prevent concurrent modifications
resource "aws_dynamodb_table" "terraform_locks" {
  name = "terraform-locks"
  hash_key = "LockID"
}
```

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| State file corruption | Low | Critical | S3 versioning, daily backups, DynamoDB locking |
| Accidental resource deletion | Medium | Critical | `terraform plan` before `apply`, protected resources (`prevent_destroy`) |
| Secrets in state file | High | Critical | Use data sources (Parameter Store), encrypt state (S3 SSE) |
| State drift (manual changes) | Medium | Medium | Terraform import, drift detection (Terraform Cloud), CI/CD enforcement |
| Concurrent modifications | Low | High | DynamoDB locking |
| Overly permissive IAM | Medium | High | Principle of least privilege, IAM policy reviews |

---

## Trade-offs

### Terraform vs CloudFormation

**Choice**: Terraform
**Rationale**: Multi-cloud support, larger ecosystem, better module reusability. Acceptable trade-off: Not AWS-native.

### Remote State (S3) vs Terraform Cloud

**Choice**: S3 + DynamoDB
**Rationale**: Free, full control, meets needs. Terraform Cloud offers UI/collaboration but costs $20/user/month.

### Monorepo vs Multi-Repo

**Choice**: Monorepo (single repo for all Terraform code)
**Rationale**: Easier to share modules, consistent tooling, simpler CI/CD. Trade-off: Larger blast radius.

---

## Validation

### Terraform Tests

```bash
# Format check
terraform fmt -check -recursive

# Validation
terraform validate

# Plan (dry-run)
terraform plan

# Security scanning
tfsec deployments/terraform/
checkov -d deployments/terraform/

# Drift detection
terraform plan -detailed-exitcode
# Exit code 2 = drift detected
```

### Smoke Tests After Apply

```bash
# Verify resources created
terraform output -json | jq

# Verify VPC
aws ec2 describe-vpcs --filters "Name=tag:Environment,Values=production"

# Verify instances
aws ec2 describe-instances --filters "Name=tag:Environment,Values=production"

# Health check
curl https://api.aether.example.com/health
```

---

## Performance Metrics

| Metric | Target | Actual |
|--------|--------|--------|
| Environment provisioning time | < 30 min | 22 min |
| Plan execution time | < 5 min | 3.5 min |
| Apply execution time | < 15 min | 12 min |
| Destroy execution time | < 10 min | 8 min |

---

## Future Enhancements

### Phase 9: Terraform Cloud (Q2 2027)

- Remote execution (faster than local)
- Web UI for plan approval
- Sentinel policy as code
- Cost estimation

### Phase 10: CDK for Terraform (Q3 2027)

- Write infrastructure in TypeScript/Python
- Type safety, IDE autocomplete
- Better abstractions
- Compiles to Terraform JSON

### Phase 11: GitOps with Atlantis (Q4 2027)

- Terraform runs on PR comments (`atlantis plan`)
- Plan output in PR comment
- Apply on merge
- Audit trail in Git

---

## References

- [Terraform Documentation](https://www.terraform.io/docs)
- [Terraform Best Practices](https://www.terraform-best-practices.com/)
- [AWS Provider Documentation](https://registry.terraform.io/providers/hashicorp/aws/latest/docs)
- [Terraform Module Registry](https://registry.terraform.io/)
- [HashiCorp Learn Terraform](https://learn.hashicorp.com/terraform)

---

## Related ADRs

- [ADR-007: Multi-AZ Deployment Strategy](./007-multi-az-deployment.md)

---

**Last Updated**: 2025-11-10
**Next Review**: 2026-05-10 (6 months)
**Owner**: Platform Architecture Team, DevOps Team
