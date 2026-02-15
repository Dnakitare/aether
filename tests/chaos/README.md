# Chaos Tests

Chaos engineering tests that validate system resilience by simulating infrastructure failures.

## Quick Start

```bash
# 1. Start test infrastructure
cd deployments/docker
docker-compose -f docker-compose.test.yml up -d

# 2. Wait for services to be healthy (~10 seconds)
docker-compose -f docker-compose.test.yml ps

# 3. Run chaos tests
cd ../..
go test -v ./tests/chaos/... -timeout 180s

# 4. Stop infrastructure
cd deployments/docker
docker-compose -f docker-compose.test.yml down
```

## Test Suites

### Service Failures (`service_failures_test.go`)
Tests graceful degradation when infrastructure fails:
- Redis connection loss
- PostgreSQL connection loss
- etcd connection loss
- Multiple simultaneous failures
- Partial recovery scenarios

```bash
go test -v ./tests/chaos/... -run TestChaos_RedisFailure
go test -v ./tests/chaos/... -run TestChaos_PostgresFailure
go test -v ./tests/chaos/... -run TestChaos_EtcdFailure
go test -v ./tests/chaos/... -run TestChaos_MultipleServiceFailures
```

### Recovery Tests (`recovery_test.go`)
Tests recovery and consistency after failures:
- Agent state persistence
- Scheduler recovery
- Backup/restore recovery
- HA leader re-election
- State consistency verification
- Data integrity checks

```bash
go test -v ./tests/chaos/... -run TestChaos_AgentRecovery
go test -v ./tests/chaos/... -run TestChaos_BackupRecovery
go test -v ./tests/chaos/... -run TestChaos_StateConsistency
go test -v ./tests/chaos/... -run TestChaos_DataIntegrity
```

### Stress Tests (`stress_test.go`)
Tests system behavior under load with failures:
- High load with failures (50 agents)
- Memory pressure (100 agents)
- Rapid failure cycles (10 cycles)
- Slow recovery scenarios (5s delay)
- Cascading failures
- Long-running failures (10s outage)

```bash
go test -v ./tests/chaos/... -run TestChaos_StressWithFailures
go test -v ./tests/chaos/... -run TestChaos_MemoryPressure
go test -v ./tests/chaos/... -run TestChaos_CascadingFailures
```

## Infrastructure Requirements

### PostgreSQL
- Port: 5433
- Database: aether_test
- User: aether
- Password: aether_test_password

### Redis
- Port: 6380
- Password: redis_test_password

### etcd
- Ports: 2379 (client), 2380 (peer)

## What These Tests Verify

### ✅ Graceful Degradation
- No panics or crashes during failures
- Clear error messages
- Core functionality continues
- Predictable service degradation

### ✅ Recovery
- Automatic reconnection
- State consistency maintained
- Data integrity preserved
- Full functionality restored

### ✅ Stress Resilience
- High concurrent load handling
- Memory pressure tolerance
- Rapid failure/recovery cycles
- Long-running failure survival

## Test Environment

The `ChaosEnvironment` provides:
- All core Aether components (Scheduler, Auth, Backup, HA)
- Infrastructure connections (PostgreSQL, Redis, etcd)
- Failure simulation helpers
- Recovery helpers
- Automatic cleanup

## Failure Simulation

```go
// Simulate Redis failure
env.SimulateRedisFailure()

// Restore Redis
env.RestoreRedis()

// Same for PostgreSQL and etcd
env.SimulatePostgresFailure()
env.RestorePostgres()

env.SimulateEtcdFailure()
env.RestoreEtcd()
```

## Writing New Chaos Tests

```go
func TestChaos_MyNewScenario(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping chaos test in short mode")
    }

    env := SetupChaosEnvironment(t)
    defer env.TearDown()

    env.SkipIfNoInfrastructure("redis") // Skip if Redis unavailable

    ctx := context.Background()

    t.Run("my test scenario", func(t *testing.T) {
        // 1. Setup initial state
        agentInfo := createTestAgentInfo(env)
        err := env.StateStore.SaveAgent(ctx, agentInfo)
        require.NoError(t, err)

        // 2. Inject failure
        err = env.SimulateRedisFailure()
        require.NoError(t, err)

        // 3. Verify graceful degradation
        err = env.StateStore.SaveAgent(ctx, agentInfo)
        assert.Error(t, err, "Should fail gracefully")

        // 4. Restore service
        err = env.RestoreRedis()
        require.NoError(t, err)

        // 5. Verify recovery
        err = env.StateStore.SaveAgent(ctx, agentInfo)
        assert.NoError(t, err, "Should work after recovery")
    })
}
```

## Best Practices

1. **Always check infrastructure**: Use `SkipIfNoInfrastructure()`
2. **Test recovery**: Every failure should have a recovery test
3. **Verify consistency**: Check data integrity after recovery
4. **Use timeouts**: Chaos tests can be slow
5. **Clean up**: Use `defer env.TearDown()`

## Troubleshooting

### Tests Skip Immediately
- Infrastructure not running
- Start Docker Compose services first
- Check service health: `docker-compose ps`

### Tests Timeout
- Increase timeout: `-timeout 300s`
- Some chaos tests intentionally wait for recovery

### Connection Refused Errors
- Check port mappings (5433, 6380, 2379)
- Verify no other services using ports
- Check Docker Compose logs

### Tests Hang
- etcd might not be responding
- Restart Docker Compose: `docker-compose down && docker-compose up -d`

## CI/CD Integration

```yaml
- name: Run chaos tests
  run: |
    docker-compose -f deployments/docker/docker-compose.test.yml up -d
    sleep 15  # Wait for services
    go test -v ./tests/chaos/... -timeout 300s
    docker-compose -f deployments/docker/docker-compose.test.yml down
```

## Metrics

- **Test Count**: 28 chaos tests
- **Operations**: 250+ operations with failures
- **Failure Injections**: 32+ failures
- **Duration**: ~165 seconds total
- **Success Rate**: >80% under load

## See Also

- [CHAOS_TESTS_SUMMARY.md](./CHAOS_TESTS_SUMMARY.md) - Detailed test documentation
- [Integration Tests](../integration/README.md) - Multi-component integration tests
- [Security Tests](../security/README.md) - Security vulnerability tests
