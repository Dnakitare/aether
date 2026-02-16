# Aether Runtime - AWS Infrastructure
# This module provisions complete Aether infrastructure on AWS
#
# Architecture:
# - VPC with public/private subnets across 3 AZs
# - ECS Fargate for container orchestration
# - RDS PostgreSQL for persistent storage
# - ElastiCache Redis for caching
# - Application Load Balancer for traffic distribution
# - CloudWatch for logging and monitoring

terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.5"
    }
  }

  # Backend configuration for state storage
  # Uncomment and configure for production use
  # backend "s3" {
  #   bucket         = "aether-terraform-state"
  #   key            = "prod/terraform.tfstate"
  #   region         = "us-east-1"
  #   encrypt        = true
  #   dynamodb_table = "aether-terraform-locks"
  # }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = merge(
      var.common_tags,
      {
        Project     = "aether-runtime"
        ManagedBy   = "terraform"
        Environment = var.environment
      }
    )
  }
}

# Generate random suffix for unique resource names
resource "random_string" "suffix" {
  length  = 8
  special = false
  upper   = false
}

locals {
  name_prefix = "${var.project_name}-${var.environment}"
  name_suffix = var.use_random_suffix ? random_string.suffix.result : ""

  # Availability zones
  azs = slice(data.aws_availability_zones.available.names, 0, var.az_count)

  # Common tags
  tags = merge(
    var.common_tags,
    {
      Environment = var.environment
      Terraform   = "true"
    }
  )
}

data "aws_availability_zones" "available" {
  state = "available"
}

# VPC Module
module "vpc" {
  source = "./modules/vpc"

  name_prefix = local.name_prefix
  name_suffix = local.name_suffix

  vpc_cidr            = var.vpc_cidr
  azs                 = local.azs
  private_subnet_cidrs = var.private_subnet_cidrs
  public_subnet_cidrs  = var.public_subnet_cidrs

  enable_nat_gateway   = var.enable_nat_gateway
  single_nat_gateway   = var.single_nat_gateway
  enable_dns_hostnames = true
  enable_dns_support   = true

  # VPC Flow Logs
  enable_flow_logs              = var.enable_vpc_flow_logs
  flow_logs_retention_days      = var.vpc_flow_logs_retention_days

  tags = local.tags
}

# RDS Module
module "rds" {
  source = "./modules/rds"

  name_prefix = local.name_prefix
  name_suffix = local.name_suffix

  vpc_id              = module.vpc.vpc_id
  subnet_ids          = module.vpc.private_subnet_ids
  allowed_cidr_blocks = module.vpc.private_subnet_cidrs

  # Database configuration
  engine_version       = var.db_engine_version
  instance_class       = var.db_instance_class
  allocated_storage    = var.db_allocated_storage
  max_allocated_storage = var.db_max_allocated_storage
  storage_encrypted    = var.db_storage_encrypted

  # Database name and credentials
  database_name       = var.db_name
  master_username     = var.db_master_username
  # Master password managed via Secrets Manager

  # High availability
  multi_az               = var.db_multi_az
  backup_retention_period = var.db_backup_retention_period
  backup_window          = var.db_backup_window
  maintenance_window     = var.db_maintenance_window

  # Performance and monitoring
  enabled_cloudwatch_logs_exports = var.db_enabled_cloudwatch_logs_exports
  performance_insights_enabled    = var.db_performance_insights_enabled

  tags = local.tags
}

# ElastiCache Module
module "elasticache" {
  source = "./modules/elasticache"

  name_prefix = local.name_prefix
  name_suffix = local.name_suffix

  vpc_id              = module.vpc.vpc_id
  subnet_ids          = module.vpc.private_subnet_ids
  allowed_cidr_blocks = module.vpc.private_subnet_cidrs

  # Redis configuration
  engine_version     = var.redis_engine_version
  node_type          = var.redis_node_type
  num_cache_nodes    = var.redis_num_cache_nodes
  parameter_group_family = var.redis_parameter_group_family

  # High availability
  automatic_failover_enabled = var.redis_automatic_failover_enabled
  multi_az_enabled          = var.redis_multi_az_enabled

  # Maintenance
  maintenance_window = var.redis_maintenance_window
  snapshot_window    = var.redis_snapshot_window
  snapshot_retention_limit = var.redis_snapshot_retention_limit

  tags = local.tags
}

# ECS Module
module "ecs" {
  source = "./modules/ecs"

  name_prefix = local.name_prefix
  name_suffix = local.name_suffix

  vpc_id             = module.vpc.vpc_id
  private_subnet_ids = module.vpc.private_subnet_ids
  public_subnet_ids  = module.vpc.public_subnet_ids

  # Container configuration
  container_image    = var.container_image
  container_cpu      = var.container_cpu
  container_memory   = var.container_memory
  desired_count      = var.ecs_desired_count
  min_capacity       = var.ecs_min_capacity
  max_capacity       = var.ecs_max_capacity

  # Database connection
  db_host            = module.rds.endpoint
  db_port            = module.rds.port
  db_name            = var.db_name
  db_secret_arn      = module.rds.master_user_secret_arn

  # Redis connection
  redis_host         = module.elasticache.primary_endpoint_address
  redis_port         = module.elasticache.port

  # Observability
  enable_execute_command = var.ecs_enable_execute_command
  log_retention_days     = var.ecs_log_retention_days

  tags = local.tags
}
