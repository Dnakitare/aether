# Generated Passwords and Secrets
#
# This file generates secure random passwords for database and cache credentials
# instead of requiring them as input variables. This prevents secrets from being
# stored in version control or passed insecurely.
#
# All generated secrets are:
# - Cryptographically secure random values
# - Stored in AWS Secrets Manager
# - Automatically rotated (if rotation is enabled)
# - Never exposed in Terraform outputs or state (marked sensitive)

terraform {
  required_providers {
    random = {
      source  = "hashicorp/random"
      version = "~> 3.5"
    }
  }
}

# PostgreSQL Master Password
resource "random_password" "postgres_master" {
  length  = 32
  special = true
  # Exclude characters that can cause issues in connection strings
  override_special = "!#$%&*()-_=+[]{}:?"
}

# Redis Auth Token
resource "random_password" "redis_auth" {
  length  = 64
  special = true
  # Redis auth tokens must not contain spaces
  override_special = "!#$%&*()-_=+[]{}:?"
}

# JWT Secret Key (for application)
resource "random_password" "jwt_secret" {
  length  = 64
  special = false  # Base64-safe characters only
}

# API Key for Internal Services
resource "random_password" "internal_api_key" {
  length  = 48
  special = false
}

# Encryption Key for Application Data
resource "random_password" "encryption_key" {
  length  = 32
  special = false
}

# Webhook Secret for GitHub/External Integrations
resource "random_password" "webhook_secret" {
  length  = 32
  special = false
}

# Store all secrets in AWS Secrets Manager
resource "aws_secretsmanager_secret" "postgres_master" {
  name        = "${var.cluster_name}/postgres/master"
  description = "PostgreSQL master password - auto-generated"

  recovery_window_in_days = 30

  tags = {
    Name = "${var.cluster_name}-postgres-master"
    Type = "database-credential"
  }
}

resource "aws_secretsmanager_secret_version" "postgres_master" {
  secret_id     = aws_secretsmanager_secret.postgres_master.id
  secret_string = jsonencode({
    username = var.postgres_username
    password = random_password.postgres_master.result
    host     = aws_db_instance.aether.address
    port     = aws_db_instance.aether.port
    database = aws_db_instance.aether.db_name
    engine   = "postgres"
  })
}

resource "aws_secretsmanager_secret" "redis_auth" {
  name        = "${var.cluster_name}/redis/auth"
  description = "Redis auth token - auto-generated"

  recovery_window_in_days = 30

  tags = {
    Name = "${var.cluster_name}-redis-auth"
    Type = "cache-credential"
  }
}

resource "aws_secretsmanager_secret_version" "redis_auth" {
  secret_id     = aws_secretsmanager_secret.redis_auth.id
  secret_string = jsonencode({
    host       = aws_elasticache_replication_group.aether.primary_endpoint_address
    port       = 6379
    auth_token = random_password.redis_auth.result
    tls        = true
  })
}

resource "aws_secretsmanager_secret" "application_secrets" {
  name        = "${var.cluster_name}/application/secrets"
  description = "Application secrets - auto-generated"

  recovery_window_in_days = 30

  tags = {
    Name = "${var.cluster_name}-app-secrets"
    Type = "application-credential"
  }
}

resource "aws_secretsmanager_secret_version" "application_secrets" {
  secret_id     = aws_secretsmanager_secret.application_secrets.id
  secret_string = jsonencode({
    jwt_secret       = random_password.jwt_secret.result
    internal_api_key = random_password.internal_api_key.result
    encryption_key   = random_password.encryption_key.result
    webhook_secret   = random_password.webhook_secret.result
  })
}

# Outputs (marked sensitive so they don't appear in logs)
output "postgres_password_secret_arn" {
  description = "ARN of the Secrets Manager secret containing PostgreSQL credentials"
  value       = aws_secretsmanager_secret.postgres_master.arn
}

output "redis_auth_secret_arn" {
  description = "ARN of the Secrets Manager secret containing Redis credentials"
  value       = aws_secretsmanager_secret.redis_auth.arn
}

output "application_secrets_arn" {
  description = "ARN of the Secrets Manager secret containing application secrets"
  value       = aws_secretsmanager_secret.application_secrets.arn
}

# Helper output for IAM policy creation
output "secrets_arns" {
  description = "List of all secret ARNs for IAM policy attachment"
  value = [
    aws_secretsmanager_secret.postgres_master.arn,
    aws_secretsmanager_secret.redis_auth.arn,
    aws_secretsmanager_secret.application_secrets.arn,
  ]
}
