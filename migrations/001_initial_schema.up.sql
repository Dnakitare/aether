-- Initial database schema for Aether
-- This migration creates all tables required for Phase 1-6

-- Tenants table
CREATE TABLE IF NOT EXISTS tenants (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    tier VARCHAR(50) NOT NULL DEFAULT 'free',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    metadata JSONB
);

CREATE INDEX IF NOT EXISTS idx_tenants_tier ON tenants (tier);

-- Agents table
CREATE TABLE IF NOT EXISTS agents (
    id VARCHAR(255) PRIMARY KEY,
    tenant_id VARCHAR(255) NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    image VARCHAR(512) NOT NULL,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    started_at TIMESTAMP WITH TIME ZONE,
    stopped_at TIMESTAMP WITH TIME ZONE,
    error TEXT,
    config JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agents_tenant_id ON agents (tenant_id);
CREATE INDEX IF NOT EXISTS idx_agents_status ON agents (status);
CREATE INDEX IF NOT EXISTS idx_agents_created_at ON agents (created_at DESC);

-- Checkpoints table
CREATE TABLE IF NOT EXISTS checkpoints (
    id BIGSERIAL PRIMARY KEY,
    agent_id VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(255) NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    state JSONB NOT NULL,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    size BIGINT NOT NULL,
    compressed BOOLEAN DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_checkpoints_agent_id ON checkpoints (agent_id);
CREATE INDEX IF NOT EXISTS idx_checkpoints_tenant_id ON checkpoints (tenant_id);
CREATE INDEX IF NOT EXISTS idx_checkpoints_created_at ON checkpoints (created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_checkpoints_agent_version ON checkpoints (agent_id, version);

-- Audit logs table
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    tenant_id VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    action VARCHAR(50) NOT NULL,
    resource VARCHAR(50) NOT NULL,
    resource_id VARCHAR(255),
    result VARCHAR(50) NOT NULL,
    ip_address INET,
    user_agent TEXT,
    details JSONB,
    error TEXT
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_id ON audit_logs (tenant_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs (timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_timestamp ON audit_logs (tenant_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs (user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs (action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource ON audit_logs (resource);

-- Quotas table
CREATE TABLE IF NOT EXISTS quotas (
    tenant_id VARCHAR(255) PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    max_agents INTEGER NOT NULL DEFAULT 10,
    max_cpu_cores INTEGER NOT NULL DEFAULT 16,
    max_memory_mb BIGINT NOT NULL DEFAULT 32768,
    max_disk_mb BIGINT NOT NULL DEFAULT 102400,
    max_network_bandwidth_mbps INTEGER NOT NULL DEFAULT 1000,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Quota usage tracking table
CREATE TABLE IF NOT EXISTS quota_usage (
    tenant_id VARCHAR(255) PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    current_agents INTEGER NOT NULL DEFAULT 0,
    current_cpu_cores INTEGER NOT NULL DEFAULT 0,
    current_memory_mb BIGINT NOT NULL DEFAULT 0,
    current_disk_mb BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- API keys table (for service accounts)
CREATE TABLE IF NOT EXISTS api_keys (
    id VARCHAR(255) PRIMARY KEY,
    tenant_id VARCHAR(255) NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    key_hash VARCHAR(255) NOT NULL UNIQUE,
    scopes TEXT[] NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE,
    revoked_at TIMESTAMP WITH TIME ZONE,
    last_used_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant_id ON api_keys (tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys (key_hash);

-- Scheduler nodes table (for distributed scheduling)
CREATE TABLE IF NOT EXISTS scheduler_nodes (
    id VARCHAR(255) PRIMARY KEY,
    hostname VARCHAR(255) NOT NULL,
    ip_address INET NOT NULL,
    capacity_cpu INTEGER NOT NULL,
    capacity_memory_mb BIGINT NOT NULL,
    available_cpu INTEGER NOT NULL,
    available_memory_mb BIGINT NOT NULL,
    status VARCHAR(50) NOT NULL,
    last_heartbeat TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    metadata JSONB
);

CREATE INDEX IF NOT EXISTS idx_scheduler_nodes_status ON scheduler_nodes (status);
CREATE INDEX IF NOT EXISTS idx_scheduler_nodes_last_heartbeat ON scheduler_nodes (last_heartbeat DESC);

-- Agent placement records (for tracking which node runs which agent)
CREATE TABLE IF NOT EXISTS agent_placements (
    agent_id VARCHAR(255) PRIMARY KEY,
    node_id VARCHAR(255) NOT NULL REFERENCES scheduler_nodes(id) ON DELETE CASCADE,
    tenant_id VARCHAR(255) NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    placed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_placements_node_id ON agent_placements (node_id);
CREATE INDEX IF NOT EXISTS idx_agent_placements_tenant_id ON agent_placements (tenant_id);

-- Comments for documentation
COMMENT ON TABLE tenants IS 'Multi-tenant isolation: each tenant has isolated resources';
COMMENT ON TABLE agents IS 'Agent instances managed by the runtime';
COMMENT ON TABLE checkpoints IS 'Point-in-time state snapshots for agent recovery';
COMMENT ON TABLE audit_logs IS 'Immutable audit trail for compliance (SOC 2, GDPR)';
COMMENT ON TABLE quotas IS 'Resource limits per tenant';
COMMENT ON TABLE quota_usage IS 'Current resource usage per tenant';
COMMENT ON TABLE api_keys IS 'Service account authentication tokens';
COMMENT ON TABLE scheduler_nodes IS 'Cluster nodes available for agent placement';
COMMENT ON TABLE agent_placements IS 'Mapping of agents to scheduler nodes';
