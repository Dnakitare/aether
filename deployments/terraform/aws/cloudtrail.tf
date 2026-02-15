# CloudTrail for Audit Logging (SOC 2 Compliance)
#
# This configuration provides comprehensive audit logging for all AWS API calls,
# enabling security monitoring, compliance, and incident investigation.
#
# Features:
# - Multi-region CloudTrail with log file validation
# - 7-year retention for SOC 2 compliance
# - Real-time CloudWatch integration
# - Security event alarms (unauthorized access, root usage, IAM changes)

# S3 Bucket for CloudTrail Logs
resource "aws_s3_bucket" "cloudtrail" {
  bucket = "${var.cluster_name}-cloudtrail-logs"

  tags = {
    Name        = "${var.cluster_name}-cloudtrail-logs"
    Purpose     = "audit-logging"
    Compliance  = "SOC2"
    Retention   = "7-years"
  }
}

resource "aws_s3_bucket_versioning" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_encryption" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.aether.arn
    }
  }
}

resource "aws_s3_bucket_public_access_block" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Lifecycle policy: Glacier after 90 days, delete after 7 years
resource "aws_s3_bucket_lifecycle_configuration" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id

  rule {
    id     = "cloudtrail-lifecycle"
    status = "Enabled"

    transition {
      days          = 90
      storage_class = "GLACIER"
    }

    transition {
      days          = 365
      storage_class = "DEEP_ARCHIVE"
    }

    expiration {
      days = 2555 # 7 years (SOC 2 requirement)
    }
  }
}

# Bucket policy for CloudTrail
resource "aws_s3_bucket_policy" "cloudtrail" {
  bucket = aws_s3_bucket.cloudtrail.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AWSCloudTrailAclCheck"
        Effect = "Allow"
        Principal = {
          Service = "cloudtrail.amazonaws.com"
        }
        Action   = "s3:GetBucketAcl"
        Resource = aws_s3_bucket.cloudtrail.arn
      },
      {
        Sid    = "AWSCloudTrailWrite"
        Effect = "Allow"
        Principal = {
          Service = "cloudtrail.amazonaws.com"
        }
        Action   = "s3:PutObject"
        Resource = "${aws_s3_bucket.cloudtrail.arn}/*"
        Condition = {
          StringEquals = {
            "s3:x-amz-acl" = "bucket-owner-full-control"
          }
        }
      }
    ]
  })
}

# CloudWatch Log Group for CloudTrail
resource "aws_cloudwatch_log_group" "cloudtrail" {
  name              = "/aws/cloudtrail/${var.cluster_name}"
  retention_in_days = var.log_retention_days

  tags = {
    Name    = "${var.cluster_name}-cloudtrail-logs"
    Purpose = "audit-logging"
  }
}

# IAM Role for CloudTrail to write to CloudWatch
resource "aws_iam_role" "cloudtrail_cloudwatch" {
  name = "${var.cluster_name}-cloudtrail-cloudwatch"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "cloudtrail.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = {
    Name = "${var.cluster_name}-cloudtrail-cloudwatch"
  }
}

resource "aws_iam_role_policy" "cloudtrail_cloudwatch" {
  name = "cloudtrail-cloudwatch-policy"
  role = aws_iam_role.cloudtrail_cloudwatch.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AWSCloudTrailCreateLogStream"
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "${aws_cloudwatch_log_group.cloudtrail.arn}:*"
      }
    ]
  })
}

# CloudTrail Trail
resource "aws_cloudtrail" "aether" {
  name                          = var.cluster_name
  s3_bucket_name                = aws_s3_bucket.cloudtrail.id
  include_global_service_events = true
  is_multi_region_trail         = true
  enable_log_file_validation    = true

  cloud_watch_logs_group_arn = "${aws_cloudwatch_log_group.cloudtrail.arn}:*"
  cloud_watch_logs_role_arn  = aws_iam_role.cloudtrail_cloudwatch.arn

  kms_key_id = aws_kms_key.aether.arn

  event_selector {
    read_write_type           = "All"
    include_management_events = true

    data_resource {
      type = "AWS::S3::Object"
      values = [
        "${aws_s3_bucket.backups.arn}/*"
      ]
    }
  }

  tags = {
    Name       = "${var.cluster_name}-trail"
    Purpose    = "audit-logging"
    Compliance = "SOC2"
  }

  depends_on = [
    aws_s3_bucket_policy.cloudtrail,
    aws_iam_role_policy.cloudtrail_cloudwatch
  ]
}

# CloudWatch Metric Filter: Unauthorized API Calls
resource "aws_cloudwatch_log_metric_filter" "unauthorized_api_calls" {
  name           = "${var.cluster_name}-unauthorized-api-calls"
  log_group_name = aws_cloudwatch_log_group.cloudtrail.name
  pattern        = "{ ($.errorCode = \"*UnauthorizedOperation\") || ($.errorCode = \"AccessDenied*\") }"

  metric_transformation {
    name      = "UnauthorizedAPICalls"
    namespace = "Aether/Security"
    value     = "1"
  }
}

resource "aws_cloudwatch_metric_alarm" "unauthorized_api_calls" {
  alarm_name          = "${var.cluster_name}-unauthorized-api-calls"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = "1"
  metric_name         = "UnauthorizedAPICalls"
  namespace           = "Aether/Security"
  period              = "300"
  statistic           = "Sum"
  threshold           = "5"
  alarm_description   = "Alert on multiple unauthorized API calls"
  treat_missing_data  = "notBreaching"

  tags = {
    Name     = "${var.cluster_name}-unauthorized-api-alarm"
    Severity = "high"
  }
}

# CloudWatch Metric Filter: Root Account Usage
resource "aws_cloudwatch_log_metric_filter" "root_usage" {
  name           = "${var.cluster_name}-root-account-usage"
  log_group_name = aws_cloudwatch_log_group.cloudtrail.name
  pattern        = "{ $.userIdentity.type = \"Root\" && $.userIdentity.invokedBy NOT EXISTS && $.eventType != \"AwsServiceEvent\" }"

  metric_transformation {
    name      = "RootAccountUsage"
    namespace = "Aether/Security"
    value     = "1"
  }
}

resource "aws_cloudwatch_metric_alarm" "root_usage" {
  alarm_name          = "${var.cluster_name}-root-account-usage"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = "1"
  metric_name         = "RootAccountUsage"
  namespace           = "Aether/Security"
  period              = "60"
  statistic           = "Sum"
  threshold           = "0"
  alarm_description   = "Alert on any root account usage"
  treat_missing_data  = "notBreaching"

  tags = {
    Name     = "${var.cluster_name}-root-usage-alarm"
    Severity = "critical"
  }
}

# CloudWatch Metric Filter: IAM Policy Changes
resource "aws_cloudwatch_log_metric_filter" "iam_policy_changes" {
  name           = "${var.cluster_name}-iam-policy-changes"
  log_group_name = aws_cloudwatch_log_group.cloudtrail.name
  pattern        = "{ ($.eventName = DeleteGroupPolicy) || ($.eventName = DeleteRolePolicy) || ($.eventName = DeleteUserPolicy) || ($.eventName = PutGroupPolicy) || ($.eventName = PutRolePolicy) || ($.eventName = PutUserPolicy) || ($.eventName = CreatePolicy) || ($.eventName = DeletePolicy) || ($.eventName = CreatePolicyVersion) || ($.eventName = DeletePolicyVersion) || ($.eventName = AttachRolePolicy) || ($.eventName = DetachRolePolicy) || ($.eventName = AttachUserPolicy) || ($.eventName = DetachUserPolicy) || ($.eventName = AttachGroupPolicy) || ($.eventName = DetachGroupPolicy) }"

  metric_transformation {
    name      = "IAMPolicyChanges"
    namespace = "Aether/Security"
    value     = "1"
  }
}

resource "aws_cloudwatch_metric_alarm" "iam_policy_changes" {
  alarm_name          = "${var.cluster_name}-iam-policy-changes"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = "1"
  metric_name         = "IAMPolicyChanges"
  namespace           = "Aether/Security"
  period              = "300"
  statistic           = "Sum"
  threshold           = "0"
  alarm_description   = "Alert on any IAM policy changes"
  treat_missing_data  = "notBreaching"

  tags = {
    Name     = "${var.cluster_name}-iam-changes-alarm"
    Severity = "high"
  }
}

# CloudWatch Metric Filter: Security Group Changes
resource "aws_cloudwatch_log_metric_filter" "security_group_changes" {
  name           = "${var.cluster_name}-security-group-changes"
  log_group_name = aws_cloudwatch_log_group.cloudtrail.name
  pattern        = "{ ($.eventName = AuthorizeSecurityGroupIngress) || ($.eventName = AuthorizeSecurityGroupEgress) || ($.eventName = RevokeSecurityGroupIngress) || ($.eventName = RevokeSecurityGroupEgress) || ($.eventName = CreateSecurityGroup) || ($.eventName = DeleteSecurityGroup) }"

  metric_transformation {
    name      = "SecurityGroupChanges"
    namespace = "Aether/Security"
    value     = "1"
  }
}

resource "aws_cloudwatch_metric_alarm" "security_group_changes" {
  alarm_name          = "${var.cluster_name}-security-group-changes"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = "1"
  metric_name         = "SecurityGroupChanges"
  namespace           = "Aether/Security"
  period              = "300"
  statistic           = "Sum"
  threshold           = "0"
  alarm_description   = "Alert on any security group changes"
  treat_missing_data  = "notBreaching"

  tags = {
    Name     = "${var.cluster_name}-sg-changes-alarm"
    Severity = "medium"
  }
}

# CloudWatch Metric Filter: KMS Key Changes
resource "aws_cloudwatch_log_metric_filter" "kms_key_changes" {
  name           = "${var.cluster_name}-kms-key-changes"
  log_group_name = aws_cloudwatch_log_group.cloudtrail.name
  pattern        = "{ ($.eventSource = kms.amazonaws.com) && (($.eventName = DisableKey) || ($.eventName = ScheduleKeyDeletion)) }"

  metric_transformation {
    name      = "KMSKeyChanges"
    namespace = "Aether/Security"
    value     = "1"
  }
}

resource "aws_cloudwatch_metric_alarm" "kms_key_changes" {
  alarm_name          = "${var.cluster_name}-kms-key-changes"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = "1"
  metric_name         = "KMSKeyChanges"
  namespace           = "Aether/Security"
  period              = "60"
  statistic           = "Sum"
  threshold           = "0"
  alarm_description   = "Alert on KMS key disable or deletion"
  treat_missing_data  = "notBreaching"

  tags = {
    Name     = "${var.cluster_name}-kms-changes-alarm"
    Severity = "critical"
  }
}

# Outputs
output "cloudtrail_trail_arn" {
  description = "ARN of the CloudTrail trail"
  value       = aws_cloudtrail.aether.arn
}

output "cloudtrail_bucket_name" {
  description = "Name of the S3 bucket storing CloudTrail logs"
  value       = aws_s3_bucket.cloudtrail.id
}

output "cloudtrail_log_group_name" {
  description = "Name of the CloudWatch log group for CloudTrail"
  value       = aws_cloudwatch_log_group.cloudtrail.name
}

output "cloudtrail_alarms" {
  description = "Map of CloudWatch alarms for security events"
  value = {
    unauthorized_api_calls  = aws_cloudwatch_metric_alarm.unauthorized_api_calls.arn
    root_usage              = aws_cloudwatch_metric_alarm.root_usage.arn
    iam_policy_changes      = aws_cloudwatch_metric_alarm.iam_policy_changes.arn
    security_group_changes  = aws_cloudwatch_metric_alarm.security_group_changes.arn
    kms_key_changes         = aws_cloudwatch_metric_alarm.kms_key_changes.arn
  }
}
