output "network_name" {
  description = "VPC network name"
  value       = google_compute_network.aether.name
}

output "network_id" {
  description = "VPC network ID"
  value       = google_compute_network.aether.id
}

output "public_subnet_name" {
  description = "Public subnet name"
  value       = google_compute_subnetwork.public.name
}

output "private_subnet_name" {
  description = "Private subnet name"
  value       = google_compute_subnetwork.private.name
}

# PostgreSQL Outputs
output "postgres_connection_name" {
  description = "Cloud SQL connection name"
  value       = google_sql_database_instance.aether.connection_name
}

output "postgres_private_ip" {
  description = "Cloud SQL private IP address"
  value       = google_sql_database_instance.aether.private_ip_address
}

output "postgres_database" {
  description = "PostgreSQL database name"
  value       = google_sql_database.aether.name
}

output "postgres_secret_id" {
  description = "Secret Manager secret ID for PostgreSQL credentials"
  value       = google_secret_manager_secret.postgres.secret_id
}

# Redis Outputs
output "redis_host" {
  description = "Memorystore Redis host"
  value       = google_redis_instance.aether.host
}

output "redis_port" {
  description = "Memorystore Redis port"
  value       = google_redis_instance.aether.port
}

output "redis_secret_id" {
  description = "Secret Manager secret ID for Redis credentials"
  value       = google_secret_manager_secret.redis.secret_id
}

# Storage Outputs
output "backup_bucket_name" {
  description = "Cloud Storage bucket name for backups"
  value       = google_storage_bucket.backups.name
}

output "backup_bucket_url" {
  description = "Cloud Storage bucket URL"
  value       = google_storage_bucket.backups.url
}

# KMS Outputs
output "kms_key_id" {
  description = "KMS crypto key ID"
  value       = google_kms_crypto_key.aether.id
}

output "kms_keyring_id" {
  description = "KMS key ring ID"
  value       = google_kms_key_ring.aether.id
}

# Connection Information
output "connection_info" {
  description = "Connection information for all services"
  value = {
    postgres = {
      host     = google_sql_database_instance.aether.private_ip_address
      port     = 5432
      database = google_sql_database.aether.name
    }
    redis = {
      host = google_redis_instance.aether.host
      port = google_redis_instance.aether.port
    }
    backup = {
      bucket = google_storage_bucket.backups.name
    }
  }
  sensitive = true
}
