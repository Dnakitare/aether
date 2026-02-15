# AWS Infrastructure for Aether
# Provisions VPC, EKS, RDS, ElastiCache, MSK, and supporting resources

terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project     = "aether"
      Environment = var.environment
      ManagedBy   = "terraform"
    }
  }
}

# VPC
module "vpc" {
  source = "../modules/networking"

  name               = "${var.cluster_name}-vpc"
  cidr_block         = var.vpc_cidr
  availability_zones = var.availability_zones

  public_subnet_cidrs  = var.public_subnet_cidrs
  private_subnet_cidrs = var.private_subnet_cidrs

  enable_nat_gateway = true
  single_nat_gateway = var.environment == "dev"
}

# EKS Cluster (optional - for K8s integration in future)
module "eks" {
  source = "../modules/kubernetes"

  count = var.enable_kubernetes ? 1 : 0

  cluster_name    = var.cluster_name
  cluster_version = var.kubernetes_version

  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnet_ids

  node_groups = {
    aether = {
      desired_size = var.node_desired_size
      min_size     = var.node_min_size
      max_size     = var.node_max_size

      instance_types = var.node_instance_types
      capacity_type  = var.node_capacity_type

      labels = {
        role = "aether-runtime"
      }

      taints = []
    }
  }
}

# RDS PostgreSQL
resource "aws_db_subnet_group" "aether" {
  name       = "${var.cluster_name}-db"
  subnet_ids = module.vpc.private_subnet_ids

  tags = {
    Name = "${var.cluster_name}-db"
  }
}

resource "aws_security_group" "rds" {
  name        = "${var.cluster_name}-rds"
  description = "Security group for RDS PostgreSQL"
  vpc_id      = module.vpc.vpc_id

  ingress {
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.cluster_name}-rds"
  }
}

resource "aws_db_instance" "aether" {
  identifier = "${var.cluster_name}-postgres"

  engine         = "postgres"
  engine_version = var.postgres_version
  instance_class = var.postgres_instance_class

  allocated_storage     = var.postgres_allocated_storage
  max_allocated_storage = var.postgres_max_allocated_storage
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name  = "aether"
  username = var.postgres_username
  password = random_password.postgres_master.result

  db_subnet_group_name   = aws_db_subnet_group.aether.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  backup_retention_period = var.backup_retention_days
  backup_window           = "03:00-04:00"
  maintenance_window      = "sun:04:00-sun:05:00"

  multi_az               = var.postgres_multi_az
  deletion_protection    = var.environment == "prod"
  skip_final_snapshot    = var.environment != "prod"
  final_snapshot_identifier = var.environment == "prod" ? "${var.cluster_name}-final-snapshot" : null

  enabled_cloudwatch_logs_exports = ["postgresql", "upgrade"]

  tags = {
    Name = "${var.cluster_name}-postgres"
  }
}

# ElastiCache Redis
resource "aws_elasticache_subnet_group" "aether" {
  name       = "${var.cluster_name}-redis"
  subnet_ids = module.vpc.private_subnet_ids
}

resource "aws_security_group" "redis" {
  name        = "${var.cluster_name}-redis"
  description = "Security group for ElastiCache Redis"
  vpc_id      = module.vpc.vpc_id

  ingress {
    from_port   = 6379
    to_port     = 6379
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.cluster_name}-redis"
  }
}

resource "aws_elasticache_replication_group" "aether" {
  replication_group_id       = "${var.cluster_name}-redis"
  replication_group_description = "Redis cluster for Aether"

  engine         = "redis"
  engine_version = var.redis_version
  node_type      = var.redis_node_type

  num_cache_clusters = var.redis_num_nodes

  parameter_group_name = "default.redis7"
  port                 = 6379

  subnet_group_name  = aws_elasticache_subnet_group.aether.name
  security_group_ids = [aws_security_group.redis.id]

  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  auth_token_enabled         = true
  auth_token                 = random_password.redis_auth.result

  automatic_failover_enabled = var.redis_num_nodes > 1
  multi_az_enabled           = var.redis_num_nodes > 1

  snapshot_retention_limit = var.backup_retention_days
  snapshot_window          = "03:00-05:00"
  maintenance_window       = "sun:05:00-sun:07:00"

  tags = {
    Name = "${var.cluster_name}-redis"
  }
}

# MSK (Managed Streaming for Apache Kafka)
resource "aws_msk_configuration" "aether" {
  name              = "${var.cluster_name}-kafka"
  kafka_versions    = [var.kafka_version]
  server_properties = <<PROPERTIES
auto.create.topics.enable=true
delete.topic.enable=true
log.retention.hours=168
compression.type=snappy
PROPERTIES
}

resource "aws_security_group" "msk" {
  name        = "${var.cluster_name}-msk"
  description = "Security group for MSK"
  vpc_id      = module.vpc.vpc_id

  ingress {
    from_port   = 9092
    to_port     = 9092
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  ingress {
    from_port   = 9094
    to_port     = 9094
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.cluster_name}-msk"
  }
}

resource "aws_msk_cluster" "aether" {
  cluster_name           = var.cluster_name
  kafka_version          = var.kafka_version
  number_of_broker_nodes = var.kafka_num_brokers

  broker_node_group_info {
    instance_type   = var.kafka_instance_type
    client_subnets  = module.vpc.private_subnet_ids
    security_groups = [aws_security_group.msk.id]

    storage_info {
      ebs_storage_info {
        volume_size = var.kafka_volume_size
      }
    }
  }

  encryption_info {
    encryption_in_transit {
      client_broker = "TLS"
      in_cluster    = true
    }

    encryption_at_rest_kms_key_arn = aws_kms_key.aether.arn
  }

  configuration_info {
    arn      = aws_msk_configuration.aether.arn
    revision = aws_msk_configuration.aether.latest_revision
  }

  logging_info {
    broker_logs {
      cloudwatch_logs {
        enabled   = true
        log_group = aws_cloudwatch_log_group.msk.name
      }
    }
  }

  tags = {
    Name = "${var.cluster_name}-kafka"
  }
}

resource "aws_cloudwatch_log_group" "msk" {
  name              = "/aws/msk/${var.cluster_name}"
  retention_in_days = var.log_retention_days
}

# KMS Key for encryption
resource "aws_kms_key" "aether" {
  description             = "KMS key for Aether cluster"
  deletion_window_in_days = 30
  enable_key_rotation     = true

  tags = {
    Name = "${var.cluster_name}-kms"
  }
}

resource "aws_kms_alias" "aether" {
  name          = "alias/${var.cluster_name}"
  target_key_id = aws_kms_key.aether.key_id
}

# S3 Bucket for backups
resource "aws_s3_bucket" "backups" {
  bucket = "${var.cluster_name}-backups"

  tags = {
    Name = "${var.cluster_name}-backups"
  }
}

resource "aws_s3_bucket_versioning" "backups" {
  bucket = aws_s3_bucket.backups.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_encryption" "backups" {
  bucket = aws_s3_bucket.backups.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.aether.arn
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id

  rule {
    id     = "delete-old-backups"
    status = "Enabled"

    expiration {
      days = var.backup_retention_days
    }
  }
}

# CloudWatch Log Groups
resource "aws_cloudwatch_log_group" "aether" {
  name              = "/aether/${var.cluster_name}"
  retention_in_days = var.log_retention_days

  tags = {
    Name = "${var.cluster_name}-logs"
  }
}

# Secrets Manager for sensitive data - moved to passwords.tf
