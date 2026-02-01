variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "environment" {
  description = "Environment (dev, staging, prod)"
  type        = string
  default     = "dev"
}

variable "cluster_name" {
  description = "Name of the Aether cluster"
  type        = string
}

# Network Configuration
variable "public_subnet_cidr" {
  description = "CIDR for public subnet"
  type        = string
  default     = "10.0.1.0/24"
}

variable "private_subnet_cidr" {
  description = "CIDR for private subnet"
  type        = string
  default     = "10.0.2.0/24"
}

variable "pod_cidr" {
  description = "CIDR for Kubernetes pods"
  type        = string
  default     = "10.1.0.0/16"
}

variable "service_cidr" {
  description = "CIDR for Kubernetes services"
  type        = string
  default     = "10.2.0.0/16"
}

variable "admin_cidrs" {
  description = "CIDR blocks allowed for admin access"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

# PostgreSQL Configuration
variable "postgres_tier" {
  description = "Cloud SQL tier"
  type        = string
  default     = "db-custom-2-7680" # 2 vCPU, 7.68GB RAM
}

variable "postgres_disk_size" {
  description = "Disk size in GB"
  type        = number
  default     = 100
}

variable "postgres_ha" {
  description = "Enable high availability (regional)"
  type        = bool
  default     = false
}

variable "postgres_username" {
  description = "PostgreSQL username"
  type        = string
  default     = "aether"
  sensitive   = true
}

variable "postgres_password" {
  description = "PostgreSQL password"
  type        = string
  sensitive   = true
}

# Redis Configuration
variable "redis_tier" {
  description = "Memorystore tier (BASIC or STANDARD_HA)"
  type        = string
  default     = "BASIC"
}

variable "redis_memory_gb" {
  description = "Redis memory size in GB"
  type        = number
  default     = 5
}

# Backup Configuration
variable "backup_retention_days" {
  description = "Number of days to retain backups"
  type        = number
  default     = 30
}
