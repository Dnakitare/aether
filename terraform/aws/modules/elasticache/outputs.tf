# ElastiCache Module Outputs

output "replication_group_id" {
  description = "ID of the replication group"
  value       = var.automatic_failover_enabled ? aws_elasticache_replication_group.main[0].id : null
}

output "cluster_id" {
  description = "ID of the cache cluster"
  value       = var.automatic_failover_enabled ? null : aws_elasticache_cluster.main[0].id
}

output "primary_endpoint_address" {
  description = "Primary endpoint address"
  value       = var.automatic_failover_enabled ? aws_elasticache_replication_group.main[0].primary_endpoint_address : aws_elasticache_cluster.main[0].cache_nodes[0].address
}

output "configuration_endpoint_address" {
  description = "Configuration endpoint address (for cluster mode)"
  value       = var.automatic_failover_enabled ? aws_elasticache_replication_group.main[0].configuration_endpoint_address : null
}

output "port" {
  description = "Port number"
  value       = 6379
}

output "security_group_id" {
  description = "Security group ID for Redis"
  value       = aws_security_group.redis.id
}

output "subnet_group_name" {
  description = "Name of the ElastiCache subnet group"
  value       = aws_elasticache_subnet_group.main.name
}

output "parameter_group_name" {
  description = "Name of the parameter group"
  value       = aws_elasticache_parameter_group.main.name
}

output "member_clusters" {
  description = "List of member cluster IDs"
  value       = var.automatic_failover_enabled ? aws_elasticache_replication_group.main[0].member_clusters : []
}
