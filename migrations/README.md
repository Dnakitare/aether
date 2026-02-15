# Database Migrations

This directory contains database schema migrations for Aether using [golang-migrate](https://github.com/golang-migrate/migrate).

## Migration Files

Migrations are numbered sequentially and have both `.up.sql` and `.down.sql` files:

- `001_initial_schema.up.sql` - Creates all tables
- `001_initial_schema.down.sql` - Drops all tables (rollback)

## Running Migrations

### Automatic (Recommended)
Migrations run automatically when the Aether daemon starts:

```bash
./aether daemon
```

The application will:
1. Check current migration version
2. Detect dirty state (if any)
3. Apply pending migrations
4. Log the final version

### Manual Migration Commands

```bash
# Apply all pending migrations
go run cmd/migrate/main.go up

# Rollback last migration (WARNING: data loss!)
go run cmd/migrate/main.go down

# Check current version
go run cmd/migrate/main.go version

# Force version (if stuck in dirty state)
go run cmd/migrate/main.go force <version>
```

## Creating New Migrations

1. **Create migration files:**
   ```bash
   # Get current max version
   ls -1 migrations/ | grep -E "^[0-9]+" | sed 's/_.*//' | sort -n | tail -1

   # Create next version
   touch migrations/002_add_feature.up.sql
   touch migrations/002_add_feature.down.sql
   ```

2. **Write up migration** (`002_add_feature.up.sql`):
   ```sql
   -- Add your schema changes here
   ALTER TABLE agents ADD COLUMN new_field VARCHAR(255);
   CREATE INDEX idx_agents_new_field ON agents (new_field);
   ```

3. **Write down migration** (`002_add_feature.down.sql`):
   ```sql
   -- Reverse your changes here
   DROP INDEX IF EXISTS idx_agents_new_field;
   ALTER TABLE agents DROP COLUMN IF EXISTS new_field;
   ```

4. **Test locally:**
   ```bash
   # Apply migration
   go run cmd/migrate/main.go up

   # Verify schema
   psql -h localhost -U aether -d aether -c "\d agents"

   # Rollback to test down migration
   go run cmd/migrate/main.go down

   # Re-apply
   go run cmd/migrate/main.go up
   ```

## Migration Best Practices

### DO
- ✅ Keep migrations small and focused
- ✅ Test both up and down migrations
- ✅ Use `IF NOT EXISTS` and `IF EXISTS` for idempotency
- ✅ Add indexes for foreign keys
- ✅ Include comments explaining complex migrations
- ✅ Commit migrations with the code that uses them

### DON'T
- ❌ Edit existing migrations after they've been applied
- ❌ Drop columns without a deprecation period
- ❌ Make breaking schema changes without a rollout plan
- ❌ Forget to test rollback migrations
- ❌ Use database-specific syntax (PostgreSQL only is OK for Aether)

## Dirty State Recovery

If a migration fails mid-execution, the database will be in a "dirty" state:

```
Error: database is in dirty state at version 3 - please fix manually
```

**Recovery steps:**

1. **Identify the problem:**
   ```sql
   -- Connect to database
   psql -h localhost -U aether -d aether

   -- Check migration state
   SELECT * FROM schema_migrations;
   ```

2. **Fix the issue:**
   - Manually complete the failed migration SQL
   - OR manually rollback the partial changes

3. **Force to correct version:**
   ```bash
   # If migration 3 completed successfully
   go run cmd/migrate/main.go force 3

   # If migration 3 failed and needs rollback
   go run cmd/migrate/main.go force 2
   ```

4. **Re-apply:**
   ```bash
   go run cmd/migrate/main.go up
   ```

## Schema Version Table

golang-migrate creates a `schema_migrations` table to track state:

```sql
-- Check migration history
SELECT * FROM schema_migrations;

-- Example output:
-- version | dirty
-- --------+-------
--       1 | false
```

- `version`: Current schema version
- `dirty`: Whether a migration failed mid-execution

## CI/CD Integration

Add to your CI pipeline:

```yaml
# .github/workflows/test.yml
- name: Run Migrations
  run: |
    docker-compose up -d postgres
    sleep 5  # Wait for postgres to be ready
    go run cmd/migrate/main.go up
    
- name: Test Rollback
  run: |
    go run cmd/migrate/main.go down
    go run cmd/migrate/main.go up
```

## Production Deployment

**Pre-deployment checklist:**
1. ✅ Test migrations in staging environment
2. ✅ Backup production database
3. ✅ Test rollback plan
4. ✅ Schedule maintenance window (if needed)
5. ✅ Monitor migration execution time

**Deployment steps:**
```bash
# 1. Backup database
pg_dump -h prod-db -U aether aether > backup-$(date +%Y%m%d-%H%M%S).sql

# 2. Run migrations
./aether daemon  # Migrations run automatically on start

# 3. Verify version
psql -h prod-db -U aether -d aether -c "SELECT * FROM schema_migrations;"

# 4. If rollback needed
go run cmd/migrate/main.go down
```

## Troubleshooting

### Migration fails with "relation already exists"
Add `IF NOT EXISTS` to CREATE statements:
```sql
CREATE TABLE IF NOT EXISTS my_table (...);
```

### Migration is slow
- Add timeout to migration runner
- Break large migrations into smaller ones
- Use concurrent index creation (PostgreSQL 11+):
  ```sql
  CREATE INDEX CONCURRENTLY idx_name ON table(column);
  ```

### Need to skip a migration
Don't skip migrations! Instead:
1. Create a new empty migration
2. Or use `force` to set version manually (危险)

## References

- [golang-migrate documentation](https://github.com/golang-migrate/migrate/tree/master/database/postgres)
- [PostgreSQL migration best practices](https://www.postgresql.org/docs/current/ddl-alter.html)
- [Zero-downtime migrations](https://github.com/braintree/pg-migrations)
