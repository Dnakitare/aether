# Terraform Backend Bootstrap

This directory contains the Terraform configuration to bootstrap the remote state backend infrastructure.

## Overview

Before using the main Terraform configuration, you must first create the S3 bucket and DynamoDB table needed for remote state storage and locking. This bootstrap configuration creates:

1. **S3 Bucket** - Stores Terraform state files with versioning and encryption
2. **DynamoDB Table** - Provides state locking to prevent concurrent modifications
3. **IAM Policy** - Grants necessary permissions for backend access

## Prerequisites

- AWS CLI configured with appropriate credentials
- Terraform >= 1.0 installed
- AWS account with permissions to create:
  - S3 buckets
  - DynamoDB tables
  - IAM policies
  - CloudWatch log groups

## Usage

### 1. Initial Setup

```bash
cd deployments/terraform/bootstrap

# Initialize Terraform (uses local state)
terraform init

# Review the planned changes
terraform plan

# Create the backend infrastructure
terraform apply
```

### 2. Note the Outputs

After `terraform apply` completes, note the output values:

```bash
terraform output -json > backend-config.json
```

These outputs include:
- `state_bucket_name` - S3 bucket name for state storage
- `lock_table_name` - DynamoDB table name for locking
- `backend_policy_arn` - IAM policy ARN for attaching to users/roles
- `backend_configuration` - Complete backend config for main Terraform

### 3. Configure Main Terraform

Update the backend configuration in `../aws/backend.tf` with the actual values:

```hcl
terraform {
  backend "s3" {
    bucket         = "<state_bucket_name from output>"
    key            = "aether/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "<lock_table_name from output>"
    encrypt        = true
  }
}
```

### 4. Migrate Existing State (if applicable)

If you have existing Terraform state in the main configuration:

```bash
cd ../aws

# Initialize with new backend
terraform init -migrate-state

# Confirm the migration
terraform state list
```

## Customization

You can customize the backend configuration using variables:

```bash
# Create terraform.tfvars
cat > terraform.tfvars <<EOF
region            = "us-west-2"
state_bucket_name = "my-company-terraform-state"
lock_table_name   = "my-company-terraform-locks"
EOF

terraform apply
```

Or pass variables on the command line:

```bash
terraform apply \
  -var="region=us-west-2" \
  -var="state_bucket_name=my-custom-bucket" \
  -var="lock_table_name=my-custom-locks"
```

## Security Considerations

### Bucket Protection

The S3 bucket is configured with:
- ✅ Versioning enabled (state history)
- ✅ Encryption at rest (AES256)
- ✅ Public access blocked
- ✅ Lifecycle policies (delete old versions after 90 days)
- ✅ `prevent_destroy` lifecycle rule

### DynamoDB Protection

The DynamoDB table is configured with:
- ✅ Point-in-time recovery
- ✅ Encryption at rest
- ✅ `prevent_destroy` lifecycle rule

### IAM Policy

The created IAM policy grants minimal permissions:
- S3: List bucket, get/put/delete objects
- DynamoDB: Get/put/delete items for locking

Attach this policy to:
- CI/CD service accounts
- Developer IAM users/roles
- Terraform Cloud/Enterprise

```bash
# Example: Attach to CI/CD role
aws iam attach-role-policy \
  --role-name ci-cd-role \
  --policy-arn $(terraform output -raw backend_policy_arn)
```

## State Management

### Viewing State

```bash
# List state resources
terraform state list

# Show specific resource
terraform state show aws_s3_bucket.terraform_state
```

### Import Existing Resources

If you already have a bucket/table:

```bash
# Import existing S3 bucket
terraform import aws_s3_bucket.terraform_state my-existing-bucket

# Import existing DynamoDB table
terraform import aws_dynamodb_table.terraform_locks my-existing-table
```

### Backup State

Always backup the bootstrap state file:

```bash
# Backup local state
cp terraform.tfstate terraform.tfstate.backup

# Upload to secure location
aws s3 cp terraform.tfstate s3://my-backup-bucket/terraform-bootstrap/
```

## Disaster Recovery

### Recovering Lost State

If you lose the bootstrap state file but resources still exist:

1. **Recreate state file:**
   ```bash
   terraform init
   terraform import aws_s3_bucket.terraform_state <bucket-name>
   terraform import aws_dynamodb_table.terraform_locks <table-name>
   terraform import aws_iam_policy.terraform_backend <policy-arn>
   ```

2. **Verify state:**
   ```bash
   terraform plan  # Should show no changes
   ```

### Deleting Resources

⚠️ **WARNING:** Deleting these resources will break Terraform for all environments!

If you must delete:

1. **Remove `prevent_destroy`:**
   ```hcl
   # In main.tf, remove or comment out:
   # lifecycle {
   #   prevent_destroy = true
   # }
   ```

2. **Destroy resources:**
   ```bash
   terraform destroy
   ```

3. **Empty S3 bucket first:**
   ```bash
   aws s3 rm s3://<bucket-name> --recursive
   terraform destroy
   ```

## Troubleshooting

### Bucket Name Already Exists

S3 bucket names are globally unique. If you get an error:

```bash
Error: creating S3 Bucket: BucketAlreadyExists
```

Solution:
```bash
terraform apply -var="state_bucket_name=aether-terraform-state-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -d'-' -f1)"
```

### State Lock Timeout

If you encounter state lock timeouts:

```bash
# Check DynamoDB for stuck locks
aws dynamodb scan --table-name <lock-table-name>

# Force unlock (use with caution!)
terraform force-unlock <lock-id>
```

### Permission Errors

Ensure your AWS credentials have these permissions:
- `s3:CreateBucket`, `s3:PutObject`, `s3:GetObject`
- `dynamodb:CreateTable`, `dynamodb:PutItem`, `dynamodb:GetItem`
- `iam:CreatePolicy`, `iam:GetPolicy`

## Cost Estimates

### S3 Bucket
- Storage: $0.023 per GB/month (first 50 TB)
- Typical state file: < 10 MB
- **Cost: ~$0.01/month**

### DynamoDB Table
- Pay-per-request billing
- Typical usage: < 100 requests/month
- **Cost: ~$0.01/month**

### Total Monthly Cost: **~$0.02/month**

## References

- [Terraform S3 Backend Documentation](https://www.terraform.io/docs/language/settings/backends/s3.html)
- [AWS S3 Best Practices](https://docs.aws.amazon.com/AmazonS3/latest/userguide/best-practices.html)
- [DynamoDB Best Practices](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/best-practices.html)

## Support

For issues or questions:
1. Check the troubleshooting section above
2. Review Terraform documentation
3. Check AWS service status
4. Open an issue in the project repository
