# Terraform Remote State Backend Configuration
#
# This configuration stores Terraform state in S3 with DynamoDB locking
# to enable safe concurrent operations and state history.
#
# Prerequisites:
# 1. S3 bucket for state storage
# 2. DynamoDB table for state locking
# 3. Proper IAM permissions
#
# Usage:
#   terraform init -backend-config="bucket=<your-bucket>"

terraform {
  backend "s3" {
    # S3 bucket for state storage (created separately)
    bucket = "aether-terraform-state"

    # State file path within the bucket
    key = "aether/terraform.tfstate"

    # AWS region for the S3 bucket
    region = "us-east-1"

    # DynamoDB table for state locking (created separately)
    dynamodb_table = "aether-terraform-locks"

    # Enable encryption at rest
    encrypt = true

    # KMS key for encryption (optional, uses aws/s3 by default)
    # kms_key_id = "arn:aws:kms:us-east-1:ACCOUNT_ID:key/KEY_ID"

    # Workspace-aware key path
    workspace_key_prefix = "workspaces"
  }
}

# Note: The backend configuration can be overridden with:
#   terraform init \
#     -backend-config="bucket=my-state-bucket" \
#     -backend-config="key=my-project/terraform.tfstate" \
#     -backend-config="region=us-west-2" \
#     -backend-config="dynamodb_table=my-locks-table"
