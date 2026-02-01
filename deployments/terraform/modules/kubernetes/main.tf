# Kubernetes (EKS) module placeholder
# Full EKS implementation deferred to post-Phase 6 (K8s integration phase)

variable "cluster_name" {
  description = "Kubernetes cluster name"
  type        = string
}

variable "cluster_version" {
  description = "Kubernetes version"
  type        = string
}

variable "vpc_id" {
  description = "VPC ID"
  type        = string
}

variable "subnet_ids" {
  description = "Subnet IDs for the cluster"
  type        = list(string)
}

variable "node_groups" {
  description = "Node group configurations"
  type        = any
  default     = {}
}

# Placeholder outputs - full EKS module will be implemented in K8s integration phase
output "cluster_id" {
  description = "Kubernetes cluster ID"
  value       = var.cluster_name
}

output "cluster_endpoint" {
  description = "Kubernetes cluster endpoint"
  value       = "https://kubernetes.example.com"
}

output "cluster_certificate_authority_data" {
  description = "Kubernetes cluster CA certificate"
  value       = "placeholder-ca-cert"
  sensitive   = true
}
