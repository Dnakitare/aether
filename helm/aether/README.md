# Aether Helm Chart

Official Helm chart for deploying Aether Runtime on Kubernetes.

## Prerequisites

- Kubernetes 1.20+
- Helm 3.8+
- PV provisioner support (for persistent storage)
- Metrics Server (for HPA)

## Installation

### Quick Start

```bash
# Add Aether Helm repository (when published)
helm repo add aether https://charts.aether.io
helm repo update

# Install with default values
helm install aether aether/aether --namespace aether --create-namespace

# Install from local chart
helm install aether ./helm/aether --namespace aether --create-namespace
```

### Custom Installation

```bash
# Create custom values file
cat <<EOF > custom-values.yaml
apiServer:
  replicaCount: 3
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 20

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: aether.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: aether-tls
      hosts:
        - aether.example.com

postgresql:
  enabled: true
  primary:
    persistence:
      size: 50Gi

redis:
  enabled: true
  replica:
    replicaCount: 2
EOF

# Install with custom values
helm install aether ./helm/aether \
  --namespace aether \
  --create-namespace \
  --values custom-values.yaml
```

## Configuration

### API Server

| Parameter | Description | Default |
|-----------|-------------|---------|
| `apiServer.enabled` | Enable API server | `true` |
| `apiServer.replicaCount` | Number of replicas | `2` |
| `apiServer.image.repository` | Image repository | `ghcr.io/dnakitare/aether` |
| `apiServer.image.tag` | Image tag | `0.2.0-beta` |
| `apiServer.service.type` | Service type | `ClusterIP` |
| `apiServer.service.port` | Service port | `8080` |
| `apiServer.resources.limits.cpu` | CPU limit | `1000m` |
| `apiServer.resources.limits.memory` | Memory limit | `2048Mi` |
| `apiServer.autoscaling.enabled` | Enable HPA | `true` |
| `apiServer.autoscaling.minReplicas` | Min replicas | `2` |
| `apiServer.autoscaling.maxReplicas` | Max replicas | `10` |

### PostgreSQL

| Parameter | Description | Default |
|-----------|-------------|---------|
| `postgresql.enabled` | Enable PostgreSQL | `true` |
| `postgresql.auth.database` | Database name | `aether` |
| `postgresql.primary.persistence.size` | Volume size | `20Gi` |

### Redis

| Parameter | Description | Default |
|-----------|-------------|---------|
| `redis.enabled` | Enable Redis | `true` |
| `redis.auth.enabled` | Enable authentication | `true` |
| `redis.master.persistence.size` | Master volume size | `8Gi` |
| `redis.replica.replicaCount` | Replica count | `2` |

### Ingress

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ingress.enabled` | Enable Ingress | `false` |
| `ingress.className` | Ingress class | `nginx` |
| `ingress.hosts` | Ingress hosts | `[]` |
| `ingress.tls` | TLS configuration | `[]` |

### Monitoring

| Parameter | Description | Default |
|-----------|-------------|---------|
| `monitoring.serviceMonitor.enabled` | Enable ServiceMonitor | `false` |
| `monitoring.serviceMonitor.interval` | Scrape interval | `30s` |
| `networkPolicy.enabled` | Enable NetworkPolicy | `false` |

## Examples

### Production Deployment

```yaml
# production-values.yaml
global:
  storageClass: "gp3"

apiServer:
  replicaCount: 3
  autoscaling:
    enabled: true
    minReplicas: 3
    maxReplicas: 20
    targetCPUUtilizationPercentage: 70
    targetMemoryUtilizationPercentage: 80

  resources:
    limits:
      cpu: 2000m
      memory: 4096Mi
    requests:
      cpu: 1000m
      memory: 2048Mi

  podDisruptionBudget:
    enabled: true
    minAvailable: 2

postgresql:
  enabled: true
  primary:
    persistence:
      size: 100Gi
    resources:
      limits:
        cpu: 2000m
        memory: 4096Mi

redis:
  enabled: true
  replica:
    replicaCount: 3
  master:
    persistence:
      size: 20Gi

ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
  hosts:
    - host: aether.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: aether-tls
      hosts:
        - aether.example.com

monitoring:
  serviceMonitor:
    enabled: true
    interval: 30s
    labels:
      prometheus: kube-prometheus

networkPolicy:
  enabled: true
```

### External Database

```yaml
# external-db-values.yaml
postgresql:
  enabled: false
  external:
    host: postgres.example.com
    port: 5432
    database: aether
    username: aether
    existingSecret: aether-db-secret
    existingSecretPasswordKey: password

redis:
  enabled: false
  external:
    host: redis.example.com
    port: 6379
    existingSecret: aether-redis-secret
    existingSecretPasswordKey: password
```

### Development Environment

```yaml
# dev-values.yaml
apiServer:
  replicaCount: 1
  autoscaling:
    enabled: false

  resources:
    limits:
      cpu: 500m
      memory: 1024Mi

postgresql:
  enabled: true
  primary:
    persistence:
      size: 10Gi

redis:
  enabled: true
  replica:
    replicaCount: 0

ingress:
  enabled: false
```

## Upgrading

```bash
# Upgrade to new version
helm upgrade aether ./helm/aether \
  --namespace aether \
  --values custom-values.yaml

# Rollback to previous version
helm rollback aether -n aether
```

## Uninstallation

```bash
# Delete release
helm uninstall aether --namespace aether

# Delete namespace (if no longer needed)
kubectl delete namespace aether
```

## Troubleshooting

### View Pod Status

```bash
kubectl get pods -n aether
```

### View Logs

```bash
# API server logs
kubectl logs -f -l app.kubernetes.io/component=api-server -n aether
```

### Debug Pod Issues

```bash
# Describe pod
kubectl describe pod <pod-name> -n aether

# Get events
kubectl get events -n aether --sort-by='.lastTimestamp'
```

### Check HPA Status

```bash
kubectl get hpa -n aether
kubectl describe hpa aether-api -n aether
```

### Test Connectivity

```bash
# Port-forward to API server
kubectl port-forward svc/aether-api 8080:8080 -n aether

# Test health endpoint
curl http://localhost:8080/health
```

## Advanced Configuration

### Custom Environment Variables

```yaml
extraEnvVars:
  - name: CUSTOM_VAR
    value: "custom_value"
  - name: ANOTHER_VAR
    valueFrom:
      secretKeyRef:
        name: my-secret
        key: my-key
```

### Init Containers

```yaml
initContainers:
  - name: wait-for-db
    image: busybox:1.35
    command: ['sh', '-c', 'until nc -z postgresql 5432; do sleep 1; done']
```

### Sidecar Containers

```yaml
sidecars:
  - name: log-forwarder
    image: fluent/fluent-bit:2.0
    volumeMounts:
      - name: logs
        mountPath: /var/log
```

### Node Affinity

```yaml
apiServer:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: kubernetes.io/arch
                operator: In
                values:
                  - amd64
```

## Security

### Pod Security

The chart uses secure defaults:
- Non-root user (UID 1000)
- Read-only root filesystem
- No privilege escalation
- Dropped capabilities

### Network Security

When `networkPolicy.enabled: true`:
- Pod-to-pod communication restricted
- Ingress only from ingress controller
- Egress to DNS, database, and cache allowed

### RBAC

Minimal RBAC permissions:
- ConfigMap and Secret read access
- Pod lifecycle management
- Service discovery

## Support

- Documentation: https://github.com/dnakitare/aether
- Issues: https://github.com/dnakitare/aether/issues
- Discussions: https://github.com/dnakitare/aether/discussions

## License

MIT License - see LICENSE file for details
