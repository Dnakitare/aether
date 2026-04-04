-- Composite index for agent placement queries by tenant (common in distributed scheduler)
CREATE INDEX IF NOT EXISTS idx_agent_placements_tenant_placed
    ON agent_placements (tenant_id, placed_at DESC);

-- Composite index for compliance audit queries (tenant + action + time)
CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_action_time
    ON audit_logs (tenant_id, action, timestamp DESC);

-- Partial index on API keys for active (non-revoked) key lookups
CREATE INDEX IF NOT EXISTS idx_api_keys_active
    ON api_keys (tenant_id, expires_at)
    WHERE revoked_at IS NULL;

-- Index for quota usage updates (frequent writes)
CREATE INDEX IF NOT EXISTS idx_quota_usage_updated
    ON quota_usage (updated_at DESC);
