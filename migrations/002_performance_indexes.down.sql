-- Rollback performance optimization indexes

DROP INDEX CONCURRENTLY IF EXISTS idx_costs_tenant_timestamp_resource;
DROP INDEX CONCURRENTLY IF EXISTS idx_costs_tenant_timestamp;
DROP INDEX CONCURRENTLY IF EXISTS idx_agents_tenant_created;
DROP INDEX CONCURRENTLY IF EXISTS idx_agents_status_created;
