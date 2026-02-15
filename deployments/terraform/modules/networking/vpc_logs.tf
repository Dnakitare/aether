# VPC Flow Logs for Network Monitoring
#
# This configuration enables VPC Flow Logs to capture all network traffic
# for security analysis, troubleshooting, and compliance.
#
# Features:
# - Captures all accepted, rejected, and all traffic
# - Stores logs in CloudWatch for querying
# - 30-day retention for cost optimization
# - Custom log format with additional metadata

# CloudWatch Log Group for VPC Flow Logs
resource "aws_cloudwatch_log_group" "vpc_flow_logs" {
  name              = "/aws/vpc/${var.name}/flow-logs"
  retention_in_days = 30

  tags = merge(
    {
      Name    = "${var.name}-vpc-flow-logs"
      Purpose = "network-monitoring"
    },
    var.tags
  )
}

# IAM Role for VPC Flow Logs
resource "aws_iam_role" "vpc_flow_logs" {
  name = "${var.name}-vpc-flow-logs"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "vpc-flow-logs.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = merge(
    {
      Name = "${var.name}-vpc-flow-logs"
    },
    var.tags
  )
}

# IAM Policy for VPC Flow Logs to write to CloudWatch
resource "aws_iam_role_policy" "vpc_flow_logs" {
  name = "vpc-flow-logs-policy"
  role = aws_iam_role.vpc_flow_logs.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents",
          "logs:DescribeLogGroups",
          "logs:DescribeLogStreams"
        ]
        Resource = "${aws_cloudwatch_log_group.vpc_flow_logs.arn}:*"
      }
    ]
  })
}

# VPC Flow Logs - Capture ALL traffic
resource "aws_flow_log" "vpc" {
  vpc_id          = aws_vpc.main.id
  traffic_type    = "ALL" # Capture accepted, rejected, and all traffic
  iam_role_arn    = aws_iam_role.vpc_flow_logs.arn
  log_destination = aws_cloudwatch_log_group.vpc_flow_logs.arn

  # Custom log format with additional metadata
  log_format = "$${version} $${account-id} $${interface-id} $${srcaddr} $${dstaddr} $${srcport} $${dstport} $${protocol} $${packets} $${bytes} $${start} $${end} $${action} $${log-status} $${vpc-id} $${subnet-id} $${instance-id} $${tcp-flags} $${type} $${pkt-srcaddr} $${pkt-dstaddr} $${region} $${az-id} $${sublocation-type} $${sublocation-id} $${pkt-src-aws-service} $${pkt-dst-aws-service} $${flow-direction} $${traffic-path}"

  tags = merge(
    {
      Name    = "${var.name}-vpc-flow-logs"
      Purpose = "network-monitoring"
    },
    var.tags
  )

  depends_on = [aws_iam_role_policy.vpc_flow_logs]
}

# CloudWatch Metric Filter: Rejected Traffic
resource "aws_cloudwatch_log_metric_filter" "rejected_traffic" {
  name           = "${var.name}-rejected-traffic"
  log_group_name = aws_cloudwatch_log_group.vpc_flow_logs.name
  pattern        = "[version, account, eni, source, destination, srcport, destport, protocol, packets, bytes, windowstart, windowend, action=REJECT, flowlogstatus]"

  metric_transformation {
    name      = "RejectedTraffic"
    namespace = "Aether/Network"
    value     = "1"
  }
}

# CloudWatch Metric Alarm: High Rejected Traffic
resource "aws_cloudwatch_metric_alarm" "rejected_traffic" {
  alarm_name          = "${var.name}-high-rejected-traffic"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = "1"
  metric_name         = "RejectedTraffic"
  namespace           = "Aether/Network"
  period              = "300"
  statistic           = "Sum"
  threshold           = "100"
  alarm_description   = "Alert on high volume of rejected network traffic"
  treat_missing_data  = "notBreaching"

  tags = merge(
    {
      Name     = "${var.name}-rejected-traffic-alarm"
      Severity = "medium"
    },
    var.tags
  )
}

# CloudWatch Metric Filter: SSH Traffic from Internet
resource "aws_cloudwatch_log_metric_filter" "ssh_from_internet" {
  name           = "${var.name}-ssh-from-internet"
  log_group_name = aws_cloudwatch_log_group.vpc_flow_logs.name
  pattern        = "[version, account, eni, source != 10.*, destination, srcport, destport=22, protocol=6, ...]"

  metric_transformation {
    name      = "SSHFromInternet"
    namespace = "Aether/Network"
    value     = "1"
  }
}

# CloudWatch Metric Alarm: SSH from Internet
resource "aws_cloudwatch_metric_alarm" "ssh_from_internet" {
  alarm_name          = "${var.name}-ssh-from-internet"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = "1"
  metric_name         = "SSHFromInternet"
  namespace           = "Aether/Network"
  period              = "300"
  statistic           = "Sum"
  threshold           = "5"
  alarm_description   = "Alert on SSH access attempts from internet"
  treat_missing_data  = "notBreaching"

  tags = merge(
    {
      Name     = "${var.name}-ssh-internet-alarm"
      Severity = "high"
    },
    var.tags
  )
}

# CloudWatch Metric Filter: RDP Traffic from Internet
resource "aws_cloudwatch_log_metric_filter" "rdp_from_internet" {
  name           = "${var.name}-rdp-from-internet"
  log_group_name = aws_cloudwatch_log_group.vpc_flow_logs.name
  pattern        = "[version, account, eni, source != 10.*, destination, srcport, destport=3389, protocol=6, ...]"

  metric_transformation {
    name      = "RDPFromInternet"
    namespace = "Aether/Network"
    value     = "1"
  }
}

# CloudWatch Metric Alarm: RDP from Internet
resource "aws_cloudwatch_metric_alarm" "rdp_from_internet" {
  alarm_name          = "${var.name}-rdp-from-internet"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = "1"
  metric_name         = "RDPFromInternet"
  namespace           = "Aether/Network"
  period              = "300"
  statistic           = "Sum"
  threshold           = "5"
  alarm_description   = "Alert on RDP access attempts from internet"
  treat_missing_data  = "notBreaching"

  tags = merge(
    {
      Name     = "${var.name}-rdp-internet-alarm"
      Severity = "high"
    },
    var.tags
  )
}

# Outputs
output "vpc_flow_log_id" {
  description = "ID of the VPC Flow Log"
  value       = aws_flow_log.vpc.id
}

output "vpc_flow_log_group_name" {
  description = "Name of the CloudWatch log group for VPC Flow Logs"
  value       = aws_cloudwatch_log_group.vpc_flow_logs.name
}

output "vpc_flow_log_group_arn" {
  description = "ARN of the CloudWatch log group for VPC Flow Logs"
  value       = aws_cloudwatch_log_group.vpc_flow_logs.arn
}

output "vpc_flow_log_alarms" {
  description = "Map of CloudWatch alarms for network security events"
  value = {
    rejected_traffic   = aws_cloudwatch_metric_alarm.rejected_traffic.arn
    ssh_from_internet  = aws_cloudwatch_metric_alarm.ssh_from_internet.arn
    rdp_from_internet  = aws_cloudwatch_metric_alarm.rdp_from_internet.arn
  }
}
