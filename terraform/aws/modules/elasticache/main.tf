# ElastiCache Module - Redis Cluster
# Creates ElastiCache Redis with replication support

locals {
  name = var.name_suffix != "" ? "${var.name_prefix}-${var.name_suffix}" : var.name_prefix
}

# Security Group for ElastiCache
resource "aws_security_group" "redis" {
  name        = "${local.name}-redis"
  description = "Security group for ElastiCache Redis"
  vpc_id      = var.vpc_id

  ingress {
    description = "Redis from private subnets"
    from_port   = 6379
    to_port     = 6379
    protocol    = "tcp"
    cidr_blocks = var.allowed_cidr_blocks
  }

  egress {
    description = "Allow all outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(
    var.tags,
    {
      Name = "${local.name}-sg-redis"
    }
  )
}

# ElastiCache Subnet Group
resource "aws_elasticache_subnet_group" "main" {
  name       = "${local.name}-redis-subnet-group"
  subnet_ids = var.subnet_ids

  tags = merge(
    var.tags,
    {
      Name = "${local.name}-redis-subnet-group"
    }
  )
}

# ElastiCache Parameter Group
resource "aws_elasticache_parameter_group" "main" {
  name   = "${local.name}-redis-params"
  family = var.parameter_group_family

  # Enable keyspace notifications for pub/sub
  parameter {
    name  = "notify-keyspace-events"
    value = "Ex"
  }

  # Set maxmemory policy
  parameter {
    name  = "maxmemory-policy"
    value = "allkeys-lru"
  }

  tags = merge(
    var.tags,
    {
      Name = "${local.name}-redis-params"
    }
  )
}

# ElastiCache Replication Group (for Multi-AZ with automatic failover)
resource "aws_elasticache_replication_group" "main" {
  count = var.automatic_failover_enabled ? 1 : 0

  replication_group_id = "${local.name}-redis"
  description          = "Redis replication group for ${local.name}"

  # Engine
  engine               = "redis"
  engine_version       = var.engine_version
  port                 = 6379
  parameter_group_name = aws_elasticache_parameter_group.main.name

  # Node configuration
  node_type            = var.node_type
  num_cache_clusters   = var.num_cache_nodes

  # Network
  subnet_group_name  = aws_elasticache_subnet_group.main.name
  security_group_ids = [aws_security_group.redis.id]

  # High Availability
  automatic_failover_enabled = var.automatic_failover_enabled
  multi_az_enabled           = var.multi_az_enabled

  # Encryption
  at_rest_encryption_enabled = var.at_rest_encryption_enabled
  transit_encryption_enabled = var.transit_encryption_enabled

  # Maintenance
  maintenance_window       = var.maintenance_window
  snapshot_window          = var.snapshot_window
  snapshot_retention_limit = var.snapshot_retention_limit

  # Notification
  notification_topic_arn = var.notification_topic_arn

  # Logging
  log_delivery_configuration {
    destination      = aws_cloudwatch_log_group.redis[0].name
    destination_type = "cloudwatch-logs"
    log_format       = "json"
    log_type         = "slow-log"
  }

  tags = merge(
    var.tags,
    {
      Name = "${local.name}-redis-replication"
    }
  )
}

# ElastiCache Cluster (for single-node or non-failover setups)
resource "aws_elasticache_cluster" "main" {
  count = var.automatic_failover_enabled ? 0 : 1

  cluster_id = "${local.name}-redis"

  # Engine
  engine               = "redis"
  engine_version       = var.engine_version
  port                 = 6379
  parameter_group_name = aws_elasticache_parameter_group.main.name

  # Node configuration
  node_type       = var.node_type
  num_cache_nodes = 1 # Single node for non-replication

  # Network
  subnet_group_name  = aws_elasticache_subnet_group.main.name
  security_group_ids = [aws_security_group.redis.id]

  # Maintenance
  maintenance_window       = var.maintenance_window
  snapshot_window          = var.snapshot_window
  snapshot_retention_limit = var.snapshot_retention_limit

  # Notification
  notification_topic_arn = var.notification_topic_arn

  tags = merge(
    var.tags,
    {
      Name = "${local.name}-redis-cluster"
    }
  )
}

# CloudWatch Log Group
resource "aws_cloudwatch_log_group" "redis" {
  count = var.automatic_failover_enabled ? 1 : 0

  name              = "/aws/elasticache/${local.name}"
  retention_in_days = var.log_retention_days

  tags = merge(
    var.tags,
    {
      Name = "${local.name}-redis-logs"
    }
  )
}

# CloudWatch Alarms
resource "aws_cloudwatch_metric_alarm" "cache_cpu" {
  alarm_name          = "${local.name}-redis-cpu-utilization"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = "2"
  metric_name         = "CPUUtilization"
  namespace           = "AWS/ElastiCache"
  period              = "300"
  statistic           = "Average"
  threshold           = "75"
  alarm_description   = "This metric monitors Redis CPU utilization"

  dimensions = {
    CacheClusterId = var.automatic_failover_enabled ? "${aws_elasticache_replication_group.main[0].id}-001" : aws_elasticache_cluster.main[0].id
  }

  tags = var.tags
}

resource "aws_cloudwatch_metric_alarm" "cache_memory" {
  alarm_name          = "${local.name}-redis-memory-utilization"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = "2"
  metric_name         = "DatabaseMemoryUsagePercentage"
  namespace           = "AWS/ElastiCache"
  period              = "300"
  statistic           = "Average"
  threshold           = "90"
  alarm_description   = "This metric monitors Redis memory utilization"

  dimensions = {
    CacheClusterId = var.automatic_failover_enabled ? "${aws_elasticache_replication_group.main[0].id}-001" : aws_elasticache_cluster.main[0].id
  }

  tags = var.tags
}
