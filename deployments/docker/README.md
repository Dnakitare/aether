# Docker Development Environment

This directory contains Docker Compose configuration for local development.

## Prerequisites

1. Docker and Docker Compose installed
2. `.env.dev` file in the project root (see Setup below)

## Setup

1. **Copy the example environment file:**
   ```bash
   cd /path/to/aether
   cp .env.dev.example .env.dev
   ```

2. **Generate secure passwords:**
   The `.env.dev.example` file contains placeholders. Generate secure passwords using:
   ```bash
   # Generate PostgreSQL password (32 chars)
   openssl rand -base64 32 | tr -d '/=+' | head -c 32

   # Generate Redis password (32 chars)
   openssl rand -base64 32 | tr -d '/=+' | head -c 32

   # Generate JWT secret (64 chars)
   openssl rand -base64 48 | tr -d '/=+' | head -c 64
   ```

3. **Edit `.env.dev`** and replace the placeholders with your generated values.

## Usage

Start services:
```bash
docker-compose -f deployments/docker/docker-compose.dev.yml up -d
```

Stop services:
```bash
docker-compose -f deployments/docker/docker-compose.dev.yml down
```

View logs:
```bash
docker-compose -f deployments/docker/docker-compose.dev.yml logs -f
```

## Services

- **Redis** (port 6379): State management and caching
- **PostgreSQL** (port 5432): Durable storage

## Security

- ⚠️ **NEVER commit `.env.dev`** - it contains secrets
- ✅ The `.env.dev` file is already in `.gitignore`
- ✅ Only commit `.env.dev.example` with placeholders

## Connecting to Services

### Redis
```bash
redis-cli -a <REDIS_PASSWORD>
```

### PostgreSQL
```bash
psql -h localhost -U aether -d aether
# Password: <POSTGRES_PASSWORD from .env.dev>
```
