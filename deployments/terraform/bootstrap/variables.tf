# Variables for Terraform Backend Bootstrap

variable "region" {
  description = "AWS region for backend resources"
  type        = string
  default     = "us-east-1"
}

variable "state_bucket_name" {
  description = "Name of the S3 bucket for Terraform state"
  type        = string
  default     = "aether-terraform-state"

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$", var.state_bucket_name))
    error_message = "Bucket name must be lowercase alphanumeric with hyphens, 3-63 characters."
  }
}

variable "lock_table_name" {
  description = "Name of the DynamoDB table for state locking"
  type        = string
  default     = "aether-terraform-locks"

  validation {
    condition     = can(regex("^[a-zA-Z0-9_.-]{3,255}$", var.lock_table_name))
    error_message = "Table name must be 3-255 characters, alphanumeric with ._- allowed."
  }
}
