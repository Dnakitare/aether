output "vpc_id" {
  description = "VPC ID"
  value       = module.vpc.vpc_id
}

output "vpc_cidr" {
  description = "VPC CIDR block"
  value       = module.vpc.vpc_cidr
}

output "public_subnet_ids" {
  description = "Public subnet IDs"
  value       = module.vpc.public_subnet_ids
}

output "private_subnet_ids" {
  description = "Private subnet IDs"
  value       = module.vpc.private_subnet_ids
}

# Kubernetes Outputs
output "eks_cluster_id" {
  description = "EKS cluster ID"
  value       = var.enable_kubernetes ? module.eks[0].cluster_id : null
}

output "eks_cluster_endpoint" {
  description = "EKS cluster endpoint"
  value       = var.enable_kubernetes ? module.eks[0].cluster_endpoint : null
}

output "eks_cluster_certificate_authority_data" {
  description = "EKS cluster CA certificate"
  value       = var.enable_kubernetes ? module.eks[0].cluster_certificate_authority_data : null
  sensitive   = true
}

# PostgreSQL Outputs
output "postgres_endpoint" {
  description = "PostgreSQL endpoint"
  value       = aws_db_instance.aether.endpoint
}

output "postgres_address" {
  description = "PostgreSQL address"
  value       = aws_db_instance.aether.address
}

output "postgres_port" {
  description = "PostgreSQL port"
  value       = aws_db_instance.aether.port
}

output "postgres_database" {
  description = "PostgreSQL database name"
  value       = aws_db_instance.aether.db_name
}

output "postgres_secret_arn" {
  description = "ARN of the Secrets Manager secret containing PostgreSQL credentials"
  value       = aws_secretsmanager_secret.postgres.arn
}

# Redis Outputs
output "redis_endpoint" {
  description = "Redis primary endpoint"
  value       = aws_elasticache_replication_group.aether.primary_endpoint_address
}

output "redis_port" {
  description = "Redis port"
  value       = 6379
}

output "redis_secret_arn" {
  description = "ARN of the Secrets Manager secret containing Redis credentials"
  value       = aws_secretsmanager_secret.redis.arn
}

# Kafka Outputs
output "kafka_bootstrap_brokers" {
  description = "MSK bootstrap brokers"
  value       = aws_msk_cluster.aether.bootstrap_brokers_tls
}

output "kafka_zookeeper_connect" {
  description = "MSK Zookeeper connection string"
  value       = aws_msk_cluster.aether.zookeeper_connect_string
}

output "kafka_cluster_arn" {
  description = "MSK cluster ARN"
  value       = aws_msk_cluster.aether.arn
}

# S3 Outputs
output "backup_bucket_name" {
  description = "S3 bucket name for backups"
  value       = aws_s3_bucket.backups.id
}

output "backup_bucket_arn" {
  description = "S3 bucket ARN for backups"
  value       = aws_s3_bucket.backups.arn
}

# KMS Outputs
output "kms_key_id" {
  description = "KMS key ID"
  value       = aws_kms_key.aether.id
}

output "kms_key_arn" {
  description = "KMS key ARN"
  value       = aws_kms_key.aether.arn
}

# CloudWatch Outputs
output "log_group_name" {
  description = "CloudWatch log group name"
  value       = aws_cloudwatch_log_group.aether.name
}

output "log_group_arn" {
  description = "CloudWatch log group ARN"
  value       = aws_cloudwatch_log_group.aether.arn
}

# Connection Information
output "connection_info" {
  description = "Connection information for all services"
  value = {
    postgres = {
      host     = aws_db_instance.aether.address
      port     = aws_db_instance.aether.port
      database = aws_db_instance.aether.db_name
    }
    redis = {
      host = aws_elasticache_replication_group.aether.primary_endpoint_address
      port = 6379
    }
    kafka = {
      brokers = aws_msk_cluster.aether.bootstrap_brokers_tls
    }
    backup = {
      bucket = aws_s3_bucket.backups.id
    }
  }
  sensitive = true
}
