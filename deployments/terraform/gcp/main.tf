# GCP Infrastructure for Aether
# Provisions VPC, GKE, Cloud SQL, Memorystore, and supporting resources

terraform {
  required_version = ">= 1.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# VPC Network
resource "google_compute_network" "aether" {
  name                    = "${var.cluster_name}-vpc"
  auto_create_subnetworks = false
}

# Subnets
resource "google_compute_subnetwork" "public" {
  name          = "${var.cluster_name}-public"
  ip_cidr_range = var.public_subnet_cidr
  region        = var.region
  network       = google_compute_network.aether.id

  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = var.pod_cidr
  }

  secondary_ip_range {
    range_name    = "services"
    ip_cidr_range = var.service_cidr
  }
}

resource "google_compute_subnetwork" "private" {
  name          = "${var.cluster_name}-private"
  ip_cidr_range = var.private_subnet_cidr
  region        = var.region
  network       = google_compute_network.aether.id

  private_ip_google_access = true
}

# Cloud NAT
resource "google_compute_router" "aether" {
  name    = "${var.cluster_name}-router"
  region  = var.region
  network = google_compute_network.aether.id
}

resource "google_compute_router_nat" "aether" {
  name   = "${var.cluster_name}-nat"
  router = google_compute_router.aether.name
  region = var.region

  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"
}

# Cloud SQL (PostgreSQL)
resource "google_sql_database_instance" "aether" {
  name             = "${var.cluster_name}-postgres"
  database_version = "POSTGRES_16"
  region           = var.region

  deletion_protection = var.environment == "prod"

  settings {
    tier              = var.postgres_tier
    availability_type = var.postgres_ha ? "REGIONAL" : "ZONAL"
    disk_size         = var.postgres_disk_size
    disk_type         = "PD_SSD"
    disk_autoresize   = true

    backup_configuration {
      enabled                        = true
      start_time                     = "03:00"
      point_in_time_recovery_enabled = true
      transaction_log_retention_days = var.backup_retention_days
    }

    ip_configuration {
      ipv4_enabled    = false
      private_network = google_compute_network.aether.id
      require_ssl     = true
    }

    database_flags {
      name  = "max_connections"
      value = "100"
    }
  }

  depends_on = [google_service_networking_connection.private_vpc_connection]
}

resource "google_sql_database" "aether" {
  name     = "aether"
  instance = google_sql_database_instance.aether.name
}

resource "google_sql_user" "aether" {
  name     = var.postgres_username
  instance = google_sql_database_instance.aether.name
  password = var.postgres_password
}

# Private Service Connection for Cloud SQL
resource "google_compute_global_address" "private_ip_address" {
  name          = "${var.cluster_name}-private-ip"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = google_compute_network.aether.id
}

resource "google_service_networking_connection" "private_vpc_connection" {
  network                 = google_compute_network.aether.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_ip_address.name]
}

# Memorystore (Redis)
resource "google_redis_instance" "aether" {
  name           = "${var.cluster_name}-redis"
  tier           = var.redis_tier
  memory_size_gb = var.redis_memory_gb
  region         = var.region

  redis_version     = "REDIS_7_0"
  display_name      = "${var.cluster_name} Redis"
  authorized_network = google_compute_network.aether.id

  auth_enabled           = true
  transit_encryption_mode = "SERVER_AUTHENTICATION"

  maintenance_policy {
    weekly_maintenance_window {
      day = "SUNDAY"
      start_time {
        hours   = 3
        minutes = 0
      }
    }
  }
}

# Cloud Storage for backups
resource "google_storage_bucket" "backups" {
  name          = "${var.cluster_name}-backups"
  location      = var.region
  force_destroy = var.environment != "prod"

  uniform_bucket_level_access = true

  versioning {
    enabled = true
  }

  encryption {
    default_kms_key_name = google_kms_crypto_key.aether.id
  }

  lifecycle_rule {
    condition {
      age = var.backup_retention_days
    }
    action {
      type = "Delete"
    }
  }
}

# KMS for encryption
resource "google_kms_key_ring" "aether" {
  name     = "${var.cluster_name}-keyring"
  location = var.region
}

resource "google_kms_crypto_key" "aether" {
  name     = "${var.cluster_name}-key"
  key_ring = google_kms_key_ring.aether.id

  rotation_period = "7776000s" # 90 days

  lifecycle {
    prevent_destroy = false
  }
}

# Secret Manager for sensitive data
resource "google_secret_manager_secret" "postgres" {
  secret_id = "${var.cluster_name}-postgres"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "postgres" {
  secret = google_secret_manager_secret.postgres.id

  secret_data = jsonencode({
    username = var.postgres_username
    password = var.postgres_password
    host     = google_sql_database_instance.aether.private_ip_address
    port     = 5432
    database = google_sql_database.aether.name
  })
}

resource "google_secret_manager_secret" "redis" {
  secret_id = "${var.cluster_name}-redis"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "redis" {
  secret = google_secret_manager_secret.redis.id

  secret_data = jsonencode({
    host      = google_redis_instance.aether.host
    port      = google_redis_instance.aether.port
    auth_string = google_redis_instance.aether.auth_string
  })
}

# Firewall rules
resource "google_compute_firewall" "allow_internal" {
  name    = "${var.cluster_name}-allow-internal"
  network = google_compute_network.aether.name

  allow {
    protocol = "tcp"
    ports    = ["0-65535"]
  }

  allow {
    protocol = "udp"
    ports    = ["0-65535"]
  }

  allow {
    protocol = "icmp"
  }

  source_ranges = [var.public_subnet_cidr, var.private_subnet_cidr]
}

resource "google_compute_firewall" "allow_ssh" {
  name    = "${var.cluster_name}-allow-ssh"
  network = google_compute_network.aether.name

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = var.admin_cidrs
}
