# Troubleshooting Guide

Common issues and solutions for Aether Runtime.

## Table of Contents

- [General Issues](#general-issues)
- [Agent Issues](#agent-issues)
- [Scheduling Issues](#scheduling-issues)
- [Database Issues](#database-issues)
- [Performance Issues](#performance-issues)
- [Network Issues](#network-issues)
- [Authentication Issues](#authentication-issues)
- [Debugging Tools](#debugging-tools)

## General Issues

### Service Won't Start

**Symptoms:**
- Container exits immediately
- "Connection refused" errors
- Crash loop backoff in Kubernetes

**Diagnosis:**
```bash
# Check logs
kubectl logs -n aether deployment/aether-api --previous

# Check events
kubectl get events -n aether --sort-by='.lastTimestamp'

# Describe pod
kubectl describe pod -n aether <pod-name>
```

**Common Causes & Solutions:**

1. **Database connection failure**
   ```bash
   # Test database connectivity
   pg_isready -h postgres.example.com -p 5432

   # Verify credentials
   PGPASSWORD=$DB_PASSWORD psql -h $DB_HOST -U $DB_USER -d $DB_NAME -c "SELECT 1"
   ```

2. **Redis connection failure**
   ```bash
   # Test Redis connectivity
   redis-cli -h redis.example.com -p 6379 -a $REDIS_PASSWORD ping
   ```

3. **Invalid configuration**
   ```bash
   # Validate config
   aether config validate

   # Check environment variables
   kubectl exec -it -n aether deployment/aether-api -- env | grep AETHER_
   ```

### Health Check Failures

**Symptoms:**
- `/health` endpoint returns 500
- Load balancer marking instances unhealthy

**Diagnosis:**
```bash
# Check health endpoint
curl -v http://localhost:8080/health

# View health check logs
kubectl logs -n aether deployment/aether-api | grep health
```

**Solutions:**

1. **Database unhealthy**
   - Check database connectivity
   - Verify connection pool settings
   - Check for long-running queries

2. **Redis unhealthy**
   - Check Redis connectivity
   - Verify authentication
   - Check memory usage

3. **Service not ready**
   - Increase `initialDelaySeconds` in liveness probe
   - Check startup logs for errors

## Agent Issues

### Agents Not Creating

**Symptoms:**
- `CreateAgent` API returns errors
- Agents stuck in pending state

**Diagnosis:**
```bash
# Check agent creation logs
kubectl logs -n aether deployment/aether-api | jq 'select(.msg | contains("create"))'

# Check scheduler queue
curl http://localhost:8080/metrics | grep scheduler_queue_depth

# View traces in Jaeger
# Look for CreateAgent spans with errors
```

**Common Causes:**

1. **Resource exhaustion**
   ```bash
   # Check VM capacity
   kubectl top nodes

   # Check pod resources
   kubectl top pods -n aether
   ```

   **Solution**: Scale up nodes or adjust resource limits

2. **Scheduler backlog**
   ```bash
   # Check queue depth
   curl http://localhost:8080/metrics | grep aether_scheduler_queue_depth
   ```

   **Solution**: Scale scheduler or increase queue size

3. **Database lock contention**
   ```sql
   -- Check for locks
   SELECT * FROM pg_locks WHERE NOT granted;

   -- Check long-running queries
   SELECT pid, now() - query_start as duration, query
   FROM pg_stat_activity
   WHERE state = 'active' AND now() - query_start > interval '1 minute';
   ```

### Agents Crashing

**Symptoms:**
- Agents fail shortly after creation
- High agent failure rate

**Diagnosis:**
```bash
# Check agent logs
kubectl logs -n aether -l app=agent --tail=100

# Check failure metrics
curl http://localhost:8080/metrics | grep aether_agents_failed_total

# View agent events
kubectl get events -n aether --field-selector involvedObject.kind=Pod
```

**Solutions:**

1. **Out of memory**
   ```yaml
   # Increase memory limits
   resources:
     limits:
       memory: 4Gi
     requests:
       memory: 2Gi
   ```

2. **Configuration errors**
   - Check agent configuration
   - Verify environment variables
   - Check secrets and configmaps

3. **Image pull failures**
   ```bash
   # Check image pull secrets
   kubectl get secret -n aether

   # Verify image exists
   docker pull <image-name>
   ```

### Agents Not Responding

**Symptoms:**
- Agents exist but don't process requests
- Timeout errors

**Diagnosis:**
```bash
# Check agent status
kubectl get pods -n aether -l type=agent

# Check agent logs
kubectl logs -n aether <agent-pod-name> --tail=50

# Check network connectivity
kubectl exec -it -n aether <agent-pod-name> -- curl http://aether-api:8080/health
```

**Solutions:**

1. **Network policy blocking traffic**
   ```bash
   # Check network policies
   kubectl get networkpolicy -n aether

   # Test connectivity
   kubectl exec -it -n aether <agent-pod> -- nc -zv aether-api 8080
   ```

2. **Agent deadlock**
   - Check for goroutine leaks
   - Review agent logs for errors
   - Restart agent

## Scheduling Issues

### Slow Scheduling

**Symptoms:**
- High `aether_scheduler_placement_duration_seconds`
- Agents take long time to start

**Diagnosis:**
```bash
# Check scheduler metrics
curl http://localhost:8080/metrics | grep scheduler_placement_duration

# View scheduler logs
kubectl logs -n aether deployment/aether-scheduler | jq 'select(.component=="scheduler")'

# Check trace spans
# Look for ScheduleAgent spans in Jaeger
```

**Solutions:**

1. **Large queue**
   ```bash
   # Check queue depth
   curl http://localhost:8080/metrics | grep scheduler_queue_depth
   ```
   - Scale scheduler horizontally
   - Increase queue workers

2. **Resource constraints**
   - Increase scheduler CPU/memory
   - Optimize placement algorithm
   - Use distributed scheduler for >5K agents

### Scheduling Failures

**Symptoms:**
- `aether_scheduler_placements_total{result="failure"}` increasing
- Agents stuck in pending

**Diagnosis:**
```bash
# Check failure rate
curl http://localhost:8080/metrics | grep 'scheduler_placements_total{result="failure"}'

# View error logs
kubectl logs -n aether deployment/aether-scheduler | jq 'select(.level=="error")'
```

**Common Causes:**

1. **No available resources**
   - Check node capacity
   - Review resource requests
   - Scale cluster

2. **Placement constraints**
   - Review affinity rules
   - Check taints and tolerations
   - Verify node selectors

## Database Issues

### Connection Pool Exhausted

**Symptoms:**
- "too many connections" errors
- High `aether_db_connections_in_use`

**Diagnosis:**
```sql
-- Check active connections
SELECT count(*) FROM pg_stat_activity;

-- Check max connections
SHOW max_connections;

-- Check connections by state
SELECT state, count(*) FROM pg_stat_activity GROUP BY state;
```

**Solutions:**

1. **Increase max connections**
   ```sql
   -- PostgreSQL config
   ALTER SYSTEM SET max_connections = 200;
   SELECT pg_reload_conf();
   ```

2. **Optimize connection pool**
   ```bash
   # Reduce pool size per instance
   export AETHER_DB_MAX_CONNECTIONS=20
   export AETHER_DB_MAX_IDLE_CONNECTIONS=5
   ```

3. **Fix connection leaks**
   - Review code for unclosed connections
   - Check for long-running transactions
   - Enable connection timeouts

### Slow Queries

**Symptoms:**
- High database latency
- API timeouts

**Diagnosis:**
```sql
-- Find slow queries
SELECT query, mean_exec_time, calls
FROM pg_stat_statements
ORDER BY mean_exec_time DESC
LIMIT 10;

-- Check for missing indexes
SELECT schemaname, tablename, attname, n_distinct
FROM pg_stats
WHERE schemaname = 'public'
  AND n_distinct > 100
  AND tablename NOT IN (
    SELECT tablename FROM pg_indexes
  );
```

**Solutions:**

1. **Add indexes**
   ```sql
   -- Example: Index on agent_id
   CREATE INDEX idx_agents_tenant_id ON agents(tenant_id);
   CREATE INDEX idx_agents_status ON agents(status);
   ```

2. **Optimize queries**
   - Use EXPLAIN ANALYZE
   - Avoid SELECT *
   - Use appropriate JOINs

3. **Vacuum and analyze**
   ```sql
   VACUUM ANALYZE agents;
   ```

### Replication Lag

**Symptoms:**
- Stale data from read replicas
- Inconsistent query results

**Diagnosis:**
```sql
-- Check replication lag
SELECT client_addr, state, sent_lsn, write_lsn, replay_lsn,
       (EXTRACT(EPOCH FROM now()) - EXTRACT(EPOCH FROM replay_timestamp))::int AS lag_seconds
FROM pg_stat_replication;
```

**Solutions:**

1. **Increase replication bandwidth**
2. **Reduce write load on primary**
3. **Use synchronous replication for critical writes**

## Performance Issues

### High Memory Usage

**Symptoms:**
- OOMKilled pods
- Memory usage >80%

**Diagnosis:**
```bash
# Check memory usage
kubectl top pods -n aether

# Get memory profile
curl http://localhost:8080/debug/pprof/heap > heap.prof
go tool pprof -http=:8081 heap.prof
```

**Solutions:**

1. **Increase memory limits**
   ```yaml
   resources:
     limits:
       memory: 8Gi
   ```

2. **Fix memory leaks**
   - Review goroutine leaks
   - Check for unbounded caches
   - Profile memory usage

3. **Optimize caching**
   ```bash
   # Reduce cache size
   export AETHER_CACHE_MAX_SIZE=1000000
   ```

### High CPU Usage

**Symptoms:**
- CPU throttling
- Slow request processing

**Diagnosis:**
```bash
# Check CPU usage
kubectl top pods -n aether

# Get CPU profile
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof -http=:8081 cpu.prof
```

**Solutions:**

1. **Scale horizontally**
   ```bash
   kubectl scale deployment aether-api --replicas=5 -n aether
   ```

2. **Optimize hot paths**
   - Review CPU profile
   - Optimize database queries
   - Use caching

3. **Increase CPU limits**
   ```yaml
   resources:
     limits:
       cpu: 4000m
   ```

### High Latency

**Symptoms:**
- P95 latency >500ms
- Timeout errors

**Diagnosis:**
```bash
# Check latency metrics
curl http://localhost:8080/metrics | grep request_duration

# View slow traces in Jaeger
# Filter by duration > 500ms

# Check database query time
curl http://localhost:8080/metrics | grep db_query_duration
```

**Solutions:**

1. **Database optimization**
   - Add indexes
   - Optimize queries
   - Use connection pooling

2. **Caching**
   - Enable Redis caching
   - Cache frequently accessed data
   - Set appropriate TTLs

3. **Horizontal scaling**
   - Add more API servers
   - Use load balancing

## Network Issues

### Connection Timeouts

**Symptoms:**
- "connection timeout" errors
- Intermittent failures

**Diagnosis:**
```bash
# Test network connectivity
kubectl exec -it -n aether <pod> -- ping aether-api

# Check DNS resolution
kubectl exec -it -n aether <pod> -- nslookup aether-api

# Test port connectivity
kubectl exec -it -n aether <pod> -- nc -zv aether-api 8080
```

**Solutions:**

1. **DNS issues**
   ```bash
   # Check CoreDNS
   kubectl logs -n kube-system -l k8s-app=kube-dns

   # Restart CoreDNS
   kubectl rollout restart deployment coredns -n kube-system
   ```

2. **Network policies**
   ```bash
   # Check network policies
   kubectl get networkpolicy -n aether -o yaml

   # Temporarily disable to test
   kubectl delete networkpolicy -n aether --all
   ```

3. **Firewall rules**
   - Check security groups
   - Verify ingress/egress rules
   - Check cloud provider firewall

### SSL/TLS Errors

**Symptoms:**
- Certificate validation failures
- "x509: certificate signed by unknown authority"

**Diagnosis:**
```bash
# Check certificate
openssl s_client -connect aether.example.com:443 -showcerts

# Verify certificate expiry
kubectl get secret aether-tls -n aether -o yaml | \
  grep tls.crt | awk '{print $2}' | base64 -d | \
  openssl x509 -noout -dates
```

**Solutions:**

1. **Renew certificate**
   ```bash
   # With cert-manager
   kubectl delete secret aether-tls -n aether
   # cert-manager will auto-renew
   ```

2. **Trust CA certificate**
   - Add CA to trust store
   - Update application to trust custom CA

## Authentication Issues

### JWT Token Errors

**Symptoms:**
- "invalid token" errors
- 401 Unauthorized

**Diagnosis:**
```bash
# Decode JWT
echo $TOKEN | cut -d. -f2 | base64 -d | jq

# Check expiration
echo $TOKEN | cut -d. -f2 | base64 -d | jq .exp | \
  xargs -I {} date -r {}
```

**Solutions:**

1. **Token expired**
   - Request new token
   - Increase token TTL

2. **Invalid signature**
   - Verify JWT secret matches
   - Check token issuer

3. **Missing claims**
   - Verify token includes required claims
   - Check token generation logic

## Debugging Tools

### Essential Commands

```bash
# View logs
kubectl logs -f -n aether deployment/aether-api

# Execute commands in pod
kubectl exec -it -n aether <pod> -- /bin/sh

# Port forward for local access
kubectl port-forward -n aether svc/aether-api 8080:8080

# View metrics
curl http://localhost:8080/metrics

# Check health
curl http://localhost:8080/health

# View traces
open http://localhost:16686  # Jaeger UI
```

### Log Analysis

```bash
# Error logs
kubectl logs -n aether deployment/aether-api | jq 'select(.level=="error")'

# Logs for specific agent
kubectl logs -n aether deployment/aether-api | jq 'select(.agent_id=="agent-123")'

# Logs with trace ID
kubectl logs -n aether deployment/aether-api | jq 'select(.trace_id=="abc123")'

# Slow requests
kubectl logs -n aether deployment/aether-api | \
  jq 'select(.duration_ms > 1000)'
```

### Performance Profiling

```bash
# CPU profile
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof -http=:8081 cpu.prof

# Memory profile
curl http://localhost:8080/debug/pprof/heap > mem.prof
go tool pprof -http=:8081 mem.prof

# Goroutine profile
curl http://localhost:8080/debug/pprof/goroutine > goroutine.prof
go tool pprof -http=:8081 goroutine.prof

# Blocking profile
curl http://localhost:8080/debug/pprof/block > block.prof
go tool pprof -http=:8081 block.prof
```

## Getting Help

If you can't resolve an issue:

1. **Check documentation**: https://aether-runtime.github.io/docs
2. **Search issues**: https://github.com/aether-runtime/aether/issues
3. **Ask community**: https://discord.gg/aether
4. **Create issue**: https://github.com/aether-runtime/aether/issues/new

When reporting issues, include:
- Aether version
- Deployment environment (K8s, Docker, etc.)
- Error messages and logs
- Steps to reproduce
- Trace IDs if available

---

**Last Updated**: February 15, 2026
**Version**: 0.2.0-beta
