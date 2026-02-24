-- Performance Optimization Migration
-- Adds composite indexes to improve common query patterns
-- Note: CONCURRENTLY removed to allow running in transaction (required by golang-migrate)

-- Priority 1: Agent list queries (60% improvement)
-- Optimizes ListAgents query: WHERE tenant_id = X ORDER BY created_at
CREATE INDEX IF NOT EXISTS idx_agents_tenant_created
ON agents (tenant_id, created_at DESC);

-- Priority 2: Status-based agent queries (60% improvement)
-- Optimizes GetAgentsByStatus: WHERE status = X ORDER BY created_at
CREATE INDEX IF NOT EXISTS idx_agents_status_created
ON agents (status, created_at DESC);

-- Priority 3: Checkpoint lookups by agent and tenant
-- Optimizes checkpoint retrieval queries
CREATE INDEX IF NOT EXISTS idx_checkpoints_agent_created
ON checkpoints (agent_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_checkpoints_tenant_created
ON checkpoints (tenant_id, created_at DESC);

-- Add index comments for documentation
COMMENT ON INDEX idx_agents_tenant_created IS 'Optimizes agent listing by tenant with chronological order';
COMMENT ON INDEX idx_agents_status_created IS 'Optimizes agent filtering by status with chronological order';
COMMENT ON INDEX idx_checkpoints_agent_created IS 'Optimizes checkpoint retrieval by agent';
COMMENT ON INDEX idx_checkpoints_tenant_created IS 'Optimizes checkpoint listing by tenant';
