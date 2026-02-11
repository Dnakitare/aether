# Production Configuration Guide

This guide covers deploying Aether in distributed mode for production environments.

## Configuration Overview

Aether uses a hierarchical configuration system:
1. **Default values** (hard-coded in `internal/config/config.go`)
2. **Config file** (YAML format)
3. **Environment variables** (highest priority, override file values)

### Environment Variable Pattern

All environment variables use the `AETHER_` prefix with underscores for nesting:

```bash
AETHER_SCHEDULER_MODE=distributed
AETHER_SCHEDULER_SCHEDULER_ID=scheduler-prod-1
AETHER_KAFKA_BROKERS=kafka-1:9092,kafka-2:9092,kafka-3:9092
```

## Production Configuration Example

### Distributed Mode (Recommended for Production)

Create `config.prod.yaml`:

```yaml
server:
  address: ":8080"
  read_timeout: 30s
  write_timeout: 30s
  enable_cors: false
  enable_auth: true

scheduler:
  mode: distributed
  scheduler_id: ${SCHEDULER_ID}      # Unique per instance
  instance_id: ${INSTANCE_ID}        # Unique per pod/container
  hostname: ${HOSTNAME}              # Pod hostname
  strategy: bin-packing
  virtual_nodes: 100                 # For consistent hashing
  heartbeat_interval: 15s
  num_workers: 20                    # Kafka consumer workers

database:
  host: ${DATABASE_HOST}
  port: 5432
  database: aether
  user: aether
  password: ${DATABASE_PASSWORD}     # From secret
  ssl_mode: require
  max_open_conns: 50
  max_idle_conns: 10
  conn_max_lifetime: 5m

redis:
  address: ${REDIS_ADDRESS}
  password: ${REDIS_PASSWORD}        # From secret
  db: 0
  node_ttl: 60s

kafka:
  brokers:
    - ${KAFKA_BROKER_1}
    - ${KAFKA_BROKER_2}
    - ${KAFKA_BROKER_3}
  topic: aether.scheduling.requests
  dlq_topic: aether.scheduling.dlq
  consumer_group: aether-schedulers
  max_retries: 3
  request_timeout: 30s
  partition_strategy: tenant         # Fair per-tenant distribution

etcd:
  endpoints:
    - ${ETCD_ENDPOINT_1}
    - ${ETCD_ENDPOINT_2}
    - ${ETCD_ENDPOINT_3}
  key_prefix: /aether/scheduler/shards
  session_ttl: 30                    # Seconds

security:
  jwt_secret_key: ${JWT_SECRET_KEY}  # From secret, min 32 chars
  jwt_token_duration: 1h
  vault_enabled: true
  vault_address: ${VAULT_ADDR}
  vault_token: ${VAULT_TOKEN}        # From secret

observability:
  log_level: info
  log_format: json
  metrics_enabled: true
  metrics_port: 9090
  metrics_path: /metrics
  tracing_enabled: true
  tracing_sample_rate: 0.1
  jaeger_endpoint: ${JAEGER_ENDPOINT}
```

## Kubernetes Deployment

### ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: aether-config
  namespace: aether
data:
  config.yaml: |
    server:
      address: ":8080"
      enable_auth: true

    scheduler:
      mode: distributed
      strategy: bin-packing
      virtual_nodes: 100
      heartbeat_interval: 15s
      num_workers: 20

    kafka:
      topic: aether.scheduling.requests
      dlq_topic: aether.scheduling.dlq
      consumer_group: aether-schedulers
      max_retries: 3
      partition_strategy: tenant

    etcd:
      key_prefix: /aether/scheduler/shards
      session_ttl: 30

    observability:
      log_level: info
      log_format: json
      metrics_enabled: true
      metrics_port: 9090
```

### Secrets

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: aether-secrets
  namespace: aether
type: Opaque
stringData:
  database-password: <generate-strong-password>
  redis-password: <generate-strong-password>
  jwt-secret-key: <generate-32-char-secret>
  vault-token: <vault-token>
```

### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: aether-scheduler
  namespace: aether
spec:
  replicas: 3  # Multiple schedulers for HA
  selector:
    matchLabels:
      app: aether-scheduler
  template:
    metadata:
      labels:
        app: aether-scheduler
    spec:
      containers:
      - name: aether
        image: aether:latest
        command: ["./aether", "server", "--config", "/etc/aether/config.yaml"]
        ports:
        - name: http
          containerPort: 8080
        - name: metrics
          containerPort: 9090
        env:
        # Scheduler identity
        - name: AETHER_SCHEDULER_SCHEDULER_ID
          value: "scheduler-prod"
        - name: AETHER_SCHEDULER_INSTANCE_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: AETHER_SCHEDULER_HOSTNAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName

        # Database
        - name: AETHER_DATABASE_HOST
          value: "postgres.aether.svc.cluster.local"
        - name: AETHER_DATABASE_PASSWORD
          valueFrom:
            secretKeyRef:
              name: aether-secrets
              key: database-password

        # Redis
        - name: AETHER_REDIS_ADDRESS
          value: "redis.aether.svc.cluster.local:6379"
        - name: AETHER_REDIS_PASSWORD
          valueFrom:
            secretKeyRef:
              name: aether-secrets
              key: redis-password

        # Kafka
        - name: AETHER_KAFKA_BROKERS
          value: "kafka-0.kafka.aether.svc.cluster.local:9092,kafka-1.kafka.aether.svc.cluster.local:9092,kafka-2.kafka.aether.svc.cluster.local:9092"

        # etcd
        - name: AETHER_ETCD_ENDPOINTS
          value: "http://etcd-0.etcd.aether.svc.cluster.local:2379,http://etcd-1.etcd.aether.svc.cluster.local:2379,http://etcd-2.etcd.aether.svc.cluster.local:2379"

        # Security
        - name: AETHER_SECURITY_JWT_SECRET_KEY
          valueFrom:
            secretKeyRef:
              name: aether-secrets
              key: jwt-secret-key
        - name: AETHER_SECURITY_VAULT_ADDRESS
          value: "http://vault.aether.svc.cluster.local:8200"
        - name: AETHER_SECURITY_VAULT_TOKEN
          valueFrom:
            secretKeyRef:
              name: aether-secrets
              key: vault-token

        # Observability
        - name: AETHER_OBSERVABILITY_JAEGER_ENDPOINT
          value: "http://jaeger-collector.aether.svc.cluster.local:14268/api/traces"

        volumeMounts:
        - name: config
          mountPath: /etc/aether

        resources:
          requests:
            memory: "512Mi"
            cpu: "500m"
          limits:
            memory: "2Gi"
            cpu: "2000m"

        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10

        readinessProbe:
          httpGet:
            path: /readiness
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5

      volumes:
      - name: config
        configMap:
          name: aether-config
```

## Infrastructure Requirements

### etcd Cluster (3 nodes minimum)

```yaml
apiVersion: v1
kind: Service
metadata:
  name: etcd
  namespace: aether
spec:
  clusterIP: None
  ports:
  - name: client
    port: 2379
  - name: peer
    port: 2380
  selector:
    app: etcd
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: etcd
  namespace: aether
spec:
  serviceName: etcd
  replicas: 3
  selector:
    matchLabels:
      app: etcd
  template:
    metadata:
      labels:
        app: etcd
    spec:
      containers:
      - name: etcd
        image: quay.io/coreos/etcd:v3.5.10
        ports:
        - name: client
          containerPort: 2379
        - name: peer
          containerPort: 2380
        env:
        - name: ETCD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: ETCD_INITIAL_CLUSTER
          value: "etcd-0=http://etcd-0.etcd:2380,etcd-1=http://etcd-1.etcd:2380,etcd-2=http://etcd-2.etcd:2380"
        - name: ETCD_INITIAL_CLUSTER_STATE
          value: "new"
        - name: ETCD_LISTEN_CLIENT_URLS
          value: "http://0.0.0.0:2379"
        - name: ETCD_ADVERTISE_CLIENT_URLS
          value: "http://$(ETCD_NAME).etcd:2379"
        - name: ETCD_LISTEN_PEER_URLS
          value: "http://0.0.0.0:2380"
        - name: ETCD_INITIAL_ADVERTISE_PEER_URLS
          value: "http://$(ETCD_NAME).etcd:2380"
        volumeMounts:
        - name: data
          mountPath: /var/lib/etcd
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 10Gi
```

### Kafka Cluster (3 brokers minimum)

Use Strimzi operator or similar for production Kafka deployment.

### Redis (with Sentinel for HA)

Use Redis Sentinel or Redis Cluster for production.

### PostgreSQL (with replication)

Use managed PostgreSQL (RDS, Cloud SQL) or deploy with replication.

## Monitoring

### Prometheus ServiceMonitor

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: aether-scheduler
  namespace: aether
spec:
  selector:
    matchLabels:
      app: aether-scheduler
  endpoints:
  - port: metrics
    interval: 30s
    path: /metrics
```

### Key Metrics to Monitor

- `scheduler_placement_duration_seconds` - Placement latency
- `scheduler_placement_total` - Total placements
- `scheduler_placement_failures_total` - Failed placements
- `kafka_consumer_lag` - Queue backlog
- `redis_operations_total` - State sync operations
- `etcd_lease_ttl_seconds` - Scheduler liveness

## Performance Tuning

### Kafka Partitions

Calculate partitions based on expected schedulers:
```
partitions = 2 to 4 × max_schedulers
```

For 5 schedulers: 10-20 partitions recommended.

### Consumer Workers

Tune based on placement complexity:
```
num_workers = 10 to 50 per scheduler
```

More workers = higher throughput but more CPU/memory.

### Redis Connection Pool

For high throughput:
```yaml
redis:
  max_active_conns: 100
  max_idle_conns: 20
```

### etcd Session TTL

Balance between failure detection speed and lease overhead:
- Fast failover: 10-15s TTL, 5s heartbeat
- Stable operation: 30s TTL, 15s heartbeat (recommended)

## Scaling Guidelines

### Horizontal Scaling

Add more scheduler instances:
```bash
kubectl scale deployment aether-scheduler --replicas=5
```

Each scheduler automatically joins the cluster via etcd.

### Capacity Planning

| Schedulers | Agents Supported | Placements/sec |
|------------|------------------|----------------|
| 1 | 1,000 | 100-200 |
| 3 | 10,000 | 300-600 |
| 5 | 50,000 | 500-1000 |
| 10 | 100,000+ | 1000+ |

## Disaster Recovery

### Backup Strategy

1. **PostgreSQL:** Daily full backups + WAL archiving
2. **Redis:** RDB snapshots every hour
3. **etcd:** Snapshot every 6 hours
4. **Kafka:** Topic data retained for 7 days

### Recovery Procedures

See `docs/operations/disaster-recovery.md` for detailed procedures.

## Security Checklist

- [ ] TLS enabled for all inter-service communication
- [ ] Network policies restrict traffic between components
- [ ] Secrets stored in Vault, not ConfigMaps
- [ ] JWT secret rotated quarterly
- [ ] Database credentials rotated monthly
- [ ] Audit logging enabled (CloudTrail/equivalent)
- [ ] RBAC properly configured for Kubernetes
- [ ] Image scanning in CI/CD pipeline

## Troubleshooting

### Scheduler Not Joining Cluster

1. Check etcd connectivity: `kubectl logs <pod> | grep etcd`
2. Verify session TTL not expiring: Check heartbeat logs
3. Check for scheduler ID conflicts: Must be unique per instance

### High Consumer Lag

1. Increase `num_workers` in config
2. Scale out schedulers horizontally
3. Check for slow placement operations (database queries)

### Frequent Failovers

1. Increase session TTL if network latency is high
2. Check etcd cluster health
3. Verify scheduler resource limits are sufficient

## Next Steps

1. Deploy to staging environment
2. Run load tests with production-like data
3. Set up monitoring dashboards
4. Create runbook for common operations
5. Plan gradual rollout strategy
