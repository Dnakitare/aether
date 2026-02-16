# VPC Outputs
output "vpc_id" {
  description = "ID of the VPC"
  value       = module.vpc.vpc_id
}

output "vpc_cidr" {
  description = "CIDR block of the VPC"
  value       = module.vpc.vpc_cidr
}

output "private_subnet_ids" {
  description = "IDs of private subnets"
  value       = module.vpc.private_subnet_ids
}

output "public_subnet_ids" {
  description = "IDs of public subnets"
  value       = module.vpc.public_subnet_ids
}

output "nat_gateway_ids" {
  description = "IDs of NAT Gateways"
  value       = module.vpc.nat_gateway_ids
}

# RDS Outputs
output "rds_endpoint" {
  description = "RDS instance endpoint"
  value       = module.rds.endpoint
}

output "rds_port" {
  description = "RDS instance port"
  value       = module.rds.port
}

output "rds_database_name" {
  description = "Database name"
  value       = module.rds.database_name
}

output "rds_master_user_secret_arn" {
  description = "ARN of the Secrets Manager secret containing master credentials"
  value       = module.rds.master_user_secret_arn
  sensitive   = true
}

output "rds_security_group_id" {
  description = "Security group ID for RDS"
  value       = module.rds.security_group_id
}

# ElastiCache Outputs
output "redis_endpoint" {
  description = "ElastiCache Redis primary endpoint"
  value       = module.elasticache.primary_endpoint_address
}

output "redis_port" {
  description = "ElastiCache Redis port"
  value       = module.elasticache.port
}

output "redis_security_group_id" {
  description = "Security group ID for Redis"
  value       = module.elasticache.security_group_id
}

# ECS Outputs
output "ecs_cluster_id" {
  description = "ID of the ECS cluster"
  value       = module.ecs.cluster_id
}

output "ecs_cluster_name" {
  description = "Name of the ECS cluster"
  value       = module.ecs.cluster_name
}

output "ecs_service_name" {
  description = "Name of the ECS service"
  value       = module.ecs.service_name
}

output "ecs_task_definition_arn" {
  description = "ARN of the ECS task definition"
  value       = module.ecs.task_definition_arn
}

output "alb_dns_name" {
  description = "DNS name of the Application Load Balancer"
  value       = module.ecs.alb_dns_name
}

output "alb_zone_id" {
  description = "Zone ID of the Application Load Balancer"
  value       = module.ecs.alb_zone_id
}

output "alb_arn" {
  description = "ARN of the Application Load Balancer"
  value       = module.ecs.alb_arn
}

output "alb_security_group_id" {
  description = "Security group ID for ALB"
  value       = module.ecs.alb_security_group_id
}

# CloudWatch Outputs
output "log_group_name" {
  description = "CloudWatch Logs group name"
  value       = module.ecs.log_group_name
}

# Connection Information
output "connection_info" {
  description = "Connection information for Aether services"
  value = {
    api_endpoint    = "http://${module.ecs.alb_dns_name}"
    database_host   = module.rds.endpoint
    database_port   = module.rds.port
    database_name   = module.rds.database_name
    redis_host      = module.elasticache.primary_endpoint_address
    redis_port      = module.elasticache.port
    ecs_cluster     = module.ecs.cluster_name
    log_group       = module.ecs.log_group_name
  }
}

# Deployment Commands
output "deployment_commands" {
  description = "Useful commands for deployment and management"
  value = {
    get_db_password = "aws secretsmanager get-secret-value --secret-id ${module.rds.master_user_secret_arn} --query SecretString --output text | jq -r .password"
    ecs_exec        = "aws ecs execute-command --cluster ${module.ecs.cluster_name} --task <TASK_ID> --container aether --interactive --command /bin/sh"
    view_logs       = "aws logs tail ${module.ecs.log_group_name} --follow"
    scale_service   = "aws ecs update-service --cluster ${module.ecs.cluster_name} --service ${module.ecs.service_name} --desired-count <COUNT>"
  }
}

# Tags
output "common_tags" {
  description = "Common tags applied to all resources"
  value       = local.tags
}
