# Operational Runbooks

Step-by-step procedures for common operational tasks and incident response.

---

## Table of Contents

### Routine Operations
- [Service Restart](#service-restart)
- [Deploying Updates](#deploying-updates)
- [Scaling Resources](#scaling-resources)
- [Adding Compute Nodes](#adding-compute-nodes)
- [Certificate Renewal](#certificate-renewal)

### Incident Response
- [Service Outage](#service-outage)
- [Database Connection Failure](#database-connection-failure)
- [High CPU Usage](#high-cpu-usage)
- [Memory Exhaustion](#memory-exhaustion)
- [Disk Space Full](#disk-space-full)
- [Agent Start Failures](#agent-start-failures)

### Maintenance
- [Database Maintenance](#database-maintenance)
- [Backup Verification](#backup-verification)
- [Log Rotation](#log-rotation)
- [Security Updates](#security-updates)

---

## Routine Operations

### Service Restart

**When to use:** Applying configuration changes, recovering from degraded state

**Impact:** Brief service interruption (< 30s with rolling restart)

**Procedure:**

1. **Verify health before restart:**
   ```bash
   curl https://api.aether.example.com/health
   ```

2. **Put service in maintenance mode (optional):**
   ```bash
   aws elbv2 modify-target-group \
     --target-group-arn arn:aws:elasticloadbalancing:... \
     --health-check-path /maintenance
   ```

3. **Perform rolling restart on control plane:**
   ```bash
   for node in $(terraform output -json | jq -r '.control_plane_ips.value[]'); do
     echo "Restarting $node..."
     ssh ec2-user@$node "sudo systemctl restart aether"
     sleep 30  # Wait for service to stabilize
     # Verify health
     curl -f https://$node:8080/health || exit 1
   done
   ```

4. **Verify all services healthy:**
   ```bash
   curl https://api.aether.example.com/readiness
   ```

5. **Exit maintenance mode:**
   ```bash
   aws elbv2 modify-target-group \
     --target-group-arn arn:aws:elasticloadbalancing:... \
     --health-check-path /health
   ```

**Rollback:** If issues occur, restart with previous configuration

---

### Deploying Updates

**Impact:** Rolling deployment with no downtime

**Procedure:**

1. **Backup current state:**
   ```bash
   aether backup create --name "pre-update-$(date +%Y%m%d-%H%M%S)"
   ```

2. **Download new version:**
   ```bash
   VERSION="1.1.0"
   wget https://github.com/dnakitare/aether/releases/download/v${VERSION}/aether-linux-amd64
   chmod +x aether-linux-amd64
   ```

3. **Test in staging:**
   ```bash
   # Deploy to staging environment first
   terraform workspace select staging
   terraform apply -var="aether_version=${VERSION}"

   # Run smoke tests
   ./scripts/smoke-test.sh staging
   ```

4. **Deploy to production (one node at a time):**
   ```bash
   for node in $(terraform output -json | jq -r '.control_plane_ips.value[]'); do
     echo "Updating $node to version $VERSION..."

     # Copy binary
     scp aether-linux-amd64 ec2-user@$node:/tmp/aether-new

     # Update
     ssh ec2-user@$node << 'EOF'
       sudo systemctl stop aether
       sudo mv /usr/local/bin/aether /usr/local/bin/aether.backup
       sudo mv /tmp/aether-new /usr/local/bin/aether
       sudo systemctl start aether
   EOF

     # Wait and verify
     sleep 30
     curl -f https://$node:8080/health || {
       echo "Health check failed, rolling back $node"
       ssh ec2-user@$node "sudo mv /usr/local/bin/aether.backup /usr/local/bin/aether; sudo systemctl restart aether"
       exit 1
     }

     echo "$node updated successfully"
   done
   ```

5. **Verify version:**
   ```bash
   curl https://api.aether.example.com/version
   ```

6. **Monitor for 1 hour:**
   ```bash
   # Watch error rates
   aws cloudwatch get-metric-statistics \
     --namespace "Aether" \
     --metric-name "APIErrors" \
     --start-time $(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%S) \
     --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
     --period 300 \
     --statistics Sum
   ```

**Rollback:**
```bash
for node in $(terraform output -json | jq -r '.control_plane_ips.value[]'); do
  ssh ec2-user@$node "sudo systemctl stop aether; sudo mv /usr/local/bin/aether.backup /usr/local/bin/aether; sudo systemctl start aether"
done
```

---

### Scaling Resources

#### Scale Up Compute Nodes

**When to use:** Increased agent workload, performance issues

**Procedure:**

1. **Check current capacity:**
   ```bash
   aws autoscaling describe-auto-scaling-groups \
     --auto-scaling-group-names "aether-compute-nodes-production"
   ```

2. **Update desired capacity:**
   ```bash
   aws autoscaling set-desired-capacity \
     --auto-scaling-group-name "aether-compute-nodes-production" \
     --desired-capacity 20
   ```

3. **Monitor new nodes joining:**
   ```bash
   watch -n 5 'aether nodes list'
   ```

4. **Verify agents can be scheduled:**
   ```bash
   # Create test agent
   curl -X POST https://api.aether.example.com/v1/agents \
     -H "Authorization: Bearer $TOKEN" \
     -d '{"name":"test","image":"alpine","resources":{"cpu_count":1,"memory_mb":512,"disk_mb":1024}}'
   ```

#### Scale Down Compute Nodes

**When to use:** Reduced load, cost optimization

**Procedure:**

1. **Check node utilization:**
   ```bash
   aether nodes list --format json | jq '.[] | {id, cpu_percent, agent_count}'
   ```

2. **Drain nodes with low utilization:**
   ```bash
   # Move agents off node
   aether nodes drain node-xyz --timeout 10m
   ```

3. **Update Auto Scaling Group:**
   ```bash
   aws autoscaling set-desired-capacity \
     --auto-scaling-group-name "aether-compute-nodes-production" \
     --desired-capacity 10
   ```

---

### Adding Compute Nodes

**When to use:** Permanent capacity increase

**Procedure:**

1. **Update Terraform configuration:**
   ```hcl
   # terraform.tfvars
   compute_node_min_count = 10
   compute_node_max_count = 100
   ```

2. **Plan changes:**
   ```bash
   terraform plan -out=scale-up.tfplan
   ```

3. **Apply:**
   ```bash
   terraform apply scale-up.tfplan
   ```

4. **Verify nodes registered:**
   ```bash
   aether nodes list
   ```

---

### Certificate Renewal

**When to use:** TLS certificates expiring (< 30 days)

**Procedure:**

1. **Check expiration:**
   ```bash
   echo | openssl s_client -servername api.aether.example.com \
     -connect api.aether.example.com:443 2>/dev/null \
     | openssl x509 -noout -dates
   ```

2. **Request new certificate (Let's Encrypt):**
   ```bash
   sudo certbot certonly \
     --dns-route53 \
     -d api.aether.example.com \
     -d *.aether.example.com
   ```

3. **Copy certificate to all nodes:**
   ```bash
   for node in $(terraform output -json | jq -r '.control_plane_ips.value[]'); do
     scp /etc/letsencrypt/live/api.aether.example.com/fullchain.pem \
         ec2-user@$node:/tmp/server.crt
     scp /etc/letsencrypt/live/api.aether.example.com/privkey.pem \
         ec2-user@$node:/tmp/server.key

     ssh ec2-user@$node << 'EOF'
       sudo mv /tmp/server.crt /etc/aether/certs/
       sudo mv /tmp/server.key /etc/aether/certs/
       sudo chmod 600 /etc/aether/certs/server.key
       sudo systemctl reload aether
   EOF
   done
   ```

4. **Verify new certificate:**
   ```bash
   echo | openssl s_client -servername api.aether.example.com \
     -connect api.aether.example.com:443 2>/dev/null \
     | openssl x509 -noout -dates
   ```

---

## Incident Response

### Service Outage

**Symptoms:** Health checks failing, 503 errors, no response

**Severity:** Critical (P1)

**Response Time:** Immediate

**Procedure:**

1. **Confirm outage:**
   ```bash
   curl -v https://api.aether.example.com/health
   # Check from multiple locations
   ```

2. **Check load balancer:**
   ```bash
   aws elbv2 describe-target-health \
     --target-group-arn arn:aws:elasticloadbalancing:...
   ```

3. **Check service status on all nodes:**
   ```bash
   for node in $(terraform output -json | jq -r '.control_plane_ips.value[]'); do
     echo "=== $node ==="
     ssh ec2-user@$node "sudo systemctl status aether"
   done
   ```

4. **Check logs for errors:**
   ```bash
   aws logs tail /aws/aether/production \
     --since 10m \
     --filter-pattern "level=error"
   ```

5. **Common fixes:**

   **If service crashed:**
   ```bash
   ssh ec2-user@$node "sudo systemctl start aether"
   ```

   **If database connection lost:**
   ```bash
   # Check database
   aws rds describe-db-instances \
     --db-instance-identifier aether-db

   # Restart service
   ssh ec2-user@$node "sudo systemctl restart aether"
   ```

   **If memory exhaustion:**
   ```bash
   # Check memory
   ssh ec2-user@$node "free -h"

   # Restart with more memory or scale up instance
   ```

6. **Escalate if not resolved in 15 minutes**

7. **Document incident:**
   - Root cause
   - Time to detection
   - Time to resolution
   - Action items

---

### Database Connection Failure

**Symptoms:** "connection refused", "too many connections", agent operations failing

**Severity:** Critical (P1)

**Procedure:**

1. **Check database status:**
   ```bash
   aws rds describe-db-instances \
     --db-instance-identifier aether-db \
     --query 'DBInstances[0].{Status:DBInstanceStatus,Available:AvailabilityZone}'
   ```

2. **Check connection count:**
   ```bash
   psql "$DATABASE_URL" -c "SELECT count(*) FROM pg_stat_activity;"
   ```

3. **If too many connections:**
   ```bash
   # Kill idle connections
   psql "$DATABASE_URL" << 'EOF'
     SELECT pg_terminate_backend(pid)
     FROM pg_stat_activity
     WHERE datname = 'aether'
       AND state = 'idle'
       AND state_change < NOW() - INTERVAL '10 minutes';
   EOF

   # Restart services with connection pooling
   ```

4. **If database is down:**
   ```bash
   # Check for failover
   aws rds describe-db-instances \
     --db-instance-identifier aether-db

   # If multi-AZ, failover should be automatic
   # Otherwise, restore from snapshot
   ```

5. **Update connection pool settings:**
   ```yaml
   # config.yaml
   database:
     max_connections: 50  # Reduce from 100
     max_idle_connections: 10
     connection_max_lifetime: 1h
   ```

---

### High CPU Usage

**Symptoms:** CPU > 80%, slow API responses, timeouts

**Severity:** High (P2)

**Procedure:**

1. **Identify the source:**
   ```bash
   # On affected node
   top -b -n 1 | head -20

   # Check Aether CPU usage
   ps aux | grep aether
   ```

2. **Check for expensive operations:**
   ```bash
   # API endpoint latency
   curl http://localhost:9090/metrics | grep api_request_duration

   # Scheduler queue length
   curl http://localhost:9090/metrics | grep scheduler_queue_length
   ```

3. **Common causes and fixes:**

   **Large number of pending agents:**
   ```bash
   # Check queue
   aether agents list --status pending

   # Scale up compute nodes
   aws autoscaling set-desired-capacity \
     --auto-scaling-group-name "aether-compute-nodes" \
     --desired-capacity 20
   ```

   **Scheduler thrashing:**
   ```bash
   # Check failed placements
   curl http://localhost:9090/metrics | grep scheduler_failed_placements

   # Adjust scheduler interval
   # config.yaml: scheduler.interval: 10s (from 5s)
   ```

   **Database query performance:**
   ```bash
   # Check slow queries
   psql "$DATABASE_URL" << 'EOF'
     SELECT query, calls, mean_exec_time, max_exec_time
     FROM pg_stat_statements
     ORDER BY mean_exec_time DESC
     LIMIT 10;
   EOF
   ```

4. **Scale vertically if needed:**
   ```bash
   # Update instance type
   terraform apply -var="control_plane_instance_type=c5.9xlarge"
   ```

---

### Memory Exhaustion

**Symptoms:** OOM kills, service crashes, swap usage high

**Severity:** High (P2)

**Procedure:**

1. **Check memory usage:**
   ```bash
   ssh ec2-user@$node << 'EOF'
     free -h
     ps aux --sort=-%mem | head -20
   EOF
   ```

2. **Check for memory leaks:**
   ```bash
   # Monitor over time
   curl http://localhost:9090/metrics | grep go_memstats_alloc_bytes

   # Check goroutine count (leak indicator)
   curl http://localhost:9090/metrics | grep go_goroutines
   ```

3. **Immediate mitigation:**
   ```bash
   # Restart service
   ssh ec2-user@$node "sudo systemctl restart aether"
   ```

4. **Long-term fixes:**
   - Update to latest version (may have memory leak fixes)
   - Increase instance size
   - Enable memory limits in config
   - Report issue to development team

---

### Disk Space Full

**Symptoms:** "no space left on device", agent creation failures, log write errors

**Severity:** High (P2)

**Procedure:**

1. **Check disk usage:**
   ```bash
   ssh ec2-user@$node "df -h"
   ```

2. **Find large files:**
   ```bash
   ssh ec2-user@$node "du -h /var/log | sort -rh | head -20"
   ```

3. **Clean up logs:**
   ```bash
   # Rotate logs
   ssh ec2-user@$node "sudo logrotate -f /etc/logrotate.conf"

   # Clean old logs
   ssh ec2-user@$node "sudo find /var/log -name '*.gz' -mtime +30 -delete"
   ```

4. **Clean up old agent data:**
   ```bash
   # Delete stopped agents older than 7 days
   aether agents list --status stopped --older-than 7d | \
     xargs -I {} aether agents delete {}
   ```

5. **Increase disk size:**
   ```bash
   # AWS: Modify volume
   aws ec2 modify-volume \
     --volume-id vol-abc123 \
     --size 200

   # Extend filesystem
   ssh ec2-user@$node "sudo xfs_growfs /"
   ```

---

### Agent Start Failures

**Symptoms:** Agents stuck in "pending" or "starting", error creating VMs

**Severity:** Medium (P3)

**Procedure:**

1. **Check agent status:**
   ```bash
   aether agents get agent-abc123
   ```

2. **Check agent logs:**
   ```bash
   aether agents logs agent-abc123
   ```

3. **Common causes:**

   **Insufficient resources:**
   ```bash
   # Check quota
   aether quotas status

   # Check node capacity
   aether nodes list
   ```

   **Image pull failure:**
   ```bash
   # Check image registry
   ssh ec2-user@$compute_node "docker pull python:3.11-slim"

   # Check network connectivity
   ssh ec2-user@$compute_node "curl -I https://docker.io"
   ```

   **Firecracker issues:**
   ```bash
   # Check Firecracker installation
   ssh ec2-user@$compute_node "firecracker --version"

   # Check /dev/kvm
   ssh ec2-user@$compute_node "ls -l /dev/kvm"
   ```

4. **Retry agent:**
   ```bash
   aether agents restart agent-abc123
   ```

---

## Maintenance

### Database Maintenance

**Frequency:** Monthly

**Procedure:**

1. **Analyze and vacuum:**
   ```bash
   psql "$DATABASE_URL" << 'EOF'
     VACUUM ANALYZE;
   EOF
   ```

2. **Check for bloat:**
   ```bash
   psql "$DATABASE_URL" << 'EOF'
     SELECT schemaname, tablename,
       pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size
     FROM pg_tables
     WHERE schemaname = 'public'
     ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
   EOF
   ```

3. **Update statistics:**
   ```bash
   psql "$DATABASE_URL" -c "ANALYZE;"
   ```

4. **Check replication lag (if multi-AZ):**
   ```bash
   aws rds describe-db-instances \
     --db-instance-identifier aether-db \
     --query 'DBInstances[0].ReadReplicaDBInstanceIdentifiers'
   ```

---

### Backup Verification

**Frequency:** Weekly

**Procedure:**

1. **List recent backups:**
   ```bash
   aether backup list --last 7d
   ```

2. **Verify backup integrity:**
   ```bash
   aether backup verify --backup-id backup-20260209-100000
   ```

3. **Test restore (to staging):**
   ```bash
   aether backup restore \
     --backup-id backup-20260209-100000 \
     --target staging \
     --dry-run
   ```

4. **Document results:**
   - Backup size
   - Restore time
   - Any issues encountered

---

### Log Rotation

**Frequency:** Daily (automatic)

**Configuration:**

```bash
# /etc/logrotate.d/aether
/var/log/aether/*.log {
    daily
    rotate 30
    compress
    delaycompress
    notifempty
    create 0640 aether aether
    sharedscripts
    postrotate
        systemctl reload aether > /dev/null 2>&1 || true
    endscript
}
```

---

### Security Updates

**Frequency:** As needed (subscribe to security advisories)

**Procedure:**

1. **Check for security updates:**
   ```bash
   ssh ec2-user@$node "sudo yum check-update --security"
   ```

2. **Apply updates (one node at a time):**
   ```bash
   for node in $(terraform output -json | jq -r '.control_plane_ips.value[]'); do
     echo "Updating $node..."
     ssh ec2-user@$node << 'EOF'
       sudo yum update -y --security
       sudo needs-restarting -r || sudo reboot
   EOF
     sleep 300  # Wait for reboot
   done
   ```

3. **Verify services after updates:**
   ```bash
   curl https://api.aether.example.com/health
   ```

---

## Emergency Contacts

| Role | Contact | Phone | Email |
|------|---------|-------|-------|
| On-Call Engineer | PagerDuty | - | oncall@example.com |
| Engineering Lead | - | +1-xxx-xxx-xxxx | eng-lead@example.com |
| SRE Lead | - | +1-xxx-xxx-xxxx | sre-lead@example.com |
| VP Engineering | - | +1-xxx-xxx-xxxx | vp-eng@example.com |

**Escalation Policy:**
1. On-call engineer (0-15 minutes)
2. Engineering lead (15-30 minutes)
3. SRE lead (30-45 minutes)
4. VP Engineering (45+ minutes)

---

## Incident Template

```markdown
# Incident Report: [Brief Description]

**Date:** YYYY-MM-DD
**Severity:** P1/P2/P3/P4
**Status:** Investigating/Mitigated/Resolved
**Duration:** X hours Y minutes
**Impact:** Number of affected users/agents

## Timeline

- HH:MM - Event occurred
- HH:MM - Alert triggered
- HH:MM - Engineer responded
- HH:MM - Root cause identified
- HH:MM - Mitigation applied
- HH:MM - Service restored
- HH:MM - Incident resolved

## Root Cause

[Detailed explanation of what caused the incident]

## Impact

- X agents affected
- Y API requests failed
- $Z revenue impact (if applicable)

## Mitigation

[What was done to restore service]

## Action Items

- [ ] Short-term fix (Owner: Name, Due: Date)
- [ ] Long-term fix (Owner: Name, Due: Date)
- [ ] Update monitoring (Owner: Name, Due: Date)
- [ ] Update runbooks (Owner: Name, Due: Date)

## Lessons Learned

[What went well, what could be improved]
```

---

**Last Updated:** 2026-02-09
