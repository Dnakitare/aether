package distributed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/pkg/api"
	"github.com/redis/go-redis/v9"
)

// NodeRegistry provides distributed node state management via Redis.
type NodeRegistry struct {
	logger *slog.Logger
	client *redis.Client
	config NodeRegistryConfig
}

// NodeRegistryConfig configures the node registry.
type NodeRegistryConfig struct {
	// RedisAddr is the Redis server address
	RedisAddr string

	// RedisPassword is the Redis password
	RedisPassword string

	// RedisDB is the Redis database number
	RedisDB int

	// NodeTTL is the time-to-live for node entries
	NodeTTL time.Duration

	// KeyPrefix for Redis keys
	KeyPrefix string
}

// DefaultNodeRegistryConfig returns default configuration.
func DefaultNodeRegistryConfig() NodeRegistryConfig {
	return NodeRegistryConfig{
		RedisAddr:     "localhost:6379",
		RedisPassword: "",
		RedisDB:       0,
		NodeTTL:       60 * time.Second,
		KeyPrefix:     "aether",
	}
}

// NewNodeRegistry creates a new node registry.
func NewNodeRegistry(logger *slog.Logger, config NodeRegistryConfig) (*NodeRegistry, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     config.RedisAddr,
		Password: config.RedisPassword,
		DB:       config.RedisDB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &NodeRegistry{
		logger: logger.With("component", "node_registry"),
		client: client,
		config: config,
	}, nil
}

// Close closes the Redis connection.
func (nr *NodeRegistry) Close() error {
	return nr.client.Close()
}

// RegisterNode registers a node in the registry.
func (nr *NodeRegistry) RegisterNode(ctx context.Context, node *scheduler.Node, schedulerID string) error {
	nodeKey := nr.nodeKey(node.ID)
	nodesSetKey := nr.nodesSetKey()
	schedulerNodesKey := nr.schedulerNodesKey(schedulerID)
	statusKey := nr.nodesByStatusKey("ready")

	// Serialize node
	data, err := nr.serializeNode(node, schedulerID)
	if err != nil {
		return fmt.Errorf("failed to serialize node: %w", err)
	}

	// Use pipeline for atomic operations
	pipe := nr.client.Pipeline()
	pipe.Set(ctx, nodeKey, data, nr.config.NodeTTL)
	pipe.SAdd(ctx, nodesSetKey, node.ID)
	pipe.SAdd(ctx, schedulerNodesKey, node.ID)
	pipe.SAdd(ctx, statusKey, node.ID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	nr.logger.InfoContext(ctx, "node registered",
		"node_id", node.ID,
		"scheduler_id", schedulerID,
	)

	return nil
}

// UnregisterNode removes a node from the registry.
func (nr *NodeRegistry) UnregisterNode(ctx context.Context, nodeID string, schedulerID string) error {
	nodeKey := nr.nodeKey(nodeID)
	nodesSetKey := nr.nodesSetKey()
	schedulerNodesKey := nr.schedulerNodesKey(schedulerID)
	statusKey := nr.nodesByStatusKey("ready")

	// Use pipeline for atomic operations
	pipe := nr.client.Pipeline()
	pipe.Del(ctx, nodeKey)
	pipe.SRem(ctx, nodesSetKey, nodeID)
	pipe.SRem(ctx, schedulerNodesKey, nodeID)
	pipe.SRem(ctx, statusKey, nodeID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to unregister node: %w", err)
	}

	nr.logger.InfoContext(ctx, "node unregistered",
		"node_id", nodeID,
		"scheduler_id", schedulerID,
	)

	return nil
}

// GetNode retrieves a node from the registry.
func (nr *NodeRegistry) GetNode(ctx context.Context, nodeID string) (*scheduler.Node, string, error) {
	nodeKey := nr.nodeKey(nodeID)

	data, err := nr.client.Get(ctx, nodeKey).Result()
	if err == redis.Nil {
		return nil, "", fmt.Errorf("node not found: %s", nodeID)
	}
	if err != nil {
		return nil, "", fmt.Errorf("failed to get node: %w", err)
	}

	node, schedulerID, err := nr.deserializeNode([]byte(data))
	if err != nil {
		return nil, "", fmt.Errorf("failed to deserialize node: %w", err)
	}

	return node, schedulerID, nil
}

// GetOwnedNodes retrieves all nodes owned by a scheduler.
func (nr *NodeRegistry) GetOwnedNodes(ctx context.Context, schedulerID string) ([]*scheduler.Node, error) {
	schedulerNodesKey := nr.schedulerNodesKey(schedulerID)

	nodeIDs, err := nr.client.SMembers(ctx, schedulerNodesKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get owned nodes: %w", err)
	}

	nodes := make([]*scheduler.Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, _, err := nr.GetNode(ctx, nodeID)
		if err != nil {
			// Node may have expired, skip
			nr.logger.WarnContext(ctx, "failed to get node",
				"node_id", nodeID,
				"error", err,
			)
			continue
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// UpdateNode updates a node's state in the registry.
func (nr *NodeRegistry) UpdateNode(ctx context.Context, node *scheduler.Node, schedulerID string) error {
	nodeKey := nr.nodeKey(node.ID)

	// Serialize node
	data, err := nr.serializeNode(node, schedulerID)
	if err != nil {
		return fmt.Errorf("failed to serialize node: %w", err)
	}

	// Update with existing TTL
	err = nr.client.Set(ctx, nodeKey, data, nr.config.NodeTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to update node: %w", err)
	}

	return nil
}

// TryAllocate attempts to atomically allocate resources on a node.
func (nr *NodeRegistry) TryAllocate(ctx context.Context, nodeID string, agentID api.AgentID, resources scheduler.Resources) (bool, error) {
	nodeKey := nr.nodeKey(nodeID)

	// Lua script for atomic check-and-allocate
	script := `
		local node_key = KEYS[1]
		local node_json = redis.call('GET', node_key)
		if not node_json then
			return {0, 'node not found'}
		end

		local node_data = cjson.decode(node_json)
		local req_cpu = tonumber(ARGV[1])
		local req_mem = tonumber(ARGV[2])
		local req_disk = tonumber(ARGV[3])
		local agent_id = ARGV[4]

		-- Check available resources
		local avail_cpu = node_data.capacity.cpu_cores - node_data.allocated.cpu_cores
		local avail_mem = node_data.capacity.memory_mb - node_data.allocated.memory_mb
		local avail_disk = node_data.capacity.disk_mb - node_data.allocated.disk_mb

		if avail_cpu < req_cpu or avail_mem < req_mem or avail_disk < req_disk then
			return {0, 'insufficient resources'}
		end

		-- Allocate resources
		node_data.allocated.cpu_cores = node_data.allocated.cpu_cores + req_cpu
		node_data.allocated.memory_mb = node_data.allocated.memory_mb + req_mem
		node_data.allocated.disk_mb = node_data.allocated.disk_mb + req_disk
		table.insert(node_data.agents, agent_id)

		-- Save back
		redis.call('SET', node_key, cjson.encode(node_data), 'KEEPTTL')
		return {1, 'success'}
	`

	result, err := nr.client.Eval(ctx, script,
		[]string{nodeKey},
		resources.CPUCores, resources.MemoryMB, resources.DiskMB, string(agentID),
	).Result()

	if err != nil {
		return false, fmt.Errorf("allocation script failed: %w", err)
	}

	res, ok := result.([]interface{})
	if !ok || len(res) < 2 {
		return false, fmt.Errorf("invalid script result")
	}

	success := res[0].(int64) == 1
	if !success {
		nr.logger.DebugContext(ctx, "allocation failed",
			"node_id", nodeID,
			"agent_id", agentID,
			"reason", res[1],
		)
	}

	return success, nil
}

// Deallocate releases resources from a node.
func (nr *NodeRegistry) Deallocate(ctx context.Context, nodeID string, agentID api.AgentID, resources scheduler.Resources) error {
	nodeKey := nr.nodeKey(nodeID)

	// Lua script for atomic deallocation
	script := `
		local node_key = KEYS[1]
		local node_json = redis.call('GET', node_key)
		if not node_json then
			return {0, 'node not found'}
		end

		local node_data = cjson.decode(node_json)
		local req_cpu = tonumber(ARGV[1])
		local req_mem = tonumber(ARGV[2])
		local req_disk = tonumber(ARGV[3])
		local agent_id = ARGV[4]

		-- Deallocate resources
		node_data.allocated.cpu_cores = math.max(0, node_data.allocated.cpu_cores - req_cpu)
		node_data.allocated.memory_mb = math.max(0, node_data.allocated.memory_mb - req_mem)
		node_data.allocated.disk_mb = math.max(0, node_data.allocated.disk_mb - req_disk)

		-- Remove agent from list
		local new_agents = {}
		for _, aid in ipairs(node_data.agents) do
			if aid ~= agent_id then
				table.insert(new_agents, aid)
			end
		end
		node_data.agents = new_agents

		-- Save back
		redis.call('SET', node_key, cjson.encode(node_data), 'KEEPTTL')
		return {1, 'success'}
	`

	result, err := nr.client.Eval(ctx, script,
		[]string{nodeKey},
		resources.CPUCores, resources.MemoryMB, resources.DiskMB, string(agentID),
	).Result()

	if err != nil {
		return fmt.Errorf("deallocation script failed: %w", err)
	}

	res, ok := result.([]interface{})
	if !ok || len(res) < 2 {
		return fmt.Errorf("invalid script result")
	}

	success := res[0].(int64) == 1
	if !success {
		return fmt.Errorf("deallocation failed: %v", res[1])
	}

	return nil
}

// RefreshTTL refreshes the TTL for a node entry.
func (nr *NodeRegistry) RefreshTTL(ctx context.Context, nodeID string) error {
	nodeKey := nr.nodeKey(nodeID)

	err := nr.client.Expire(ctx, nodeKey, nr.config.NodeTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to refresh TTL: %w", err)
	}

	return nil
}

// ListAllNodes returns all registered nodes.
func (nr *NodeRegistry) ListAllNodes(ctx context.Context) ([]*scheduler.Node, error) {
	nodesSetKey := nr.nodesSetKey()

	nodeIDs, err := nr.client.SMembers(ctx, nodesSetKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	nodes := make([]*scheduler.Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, _, err := nr.GetNode(ctx, nodeID)
		if err != nil {
			// Node may have expired, skip
			continue
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// nodeKey returns the Redis key for a node.
func (nr *NodeRegistry) nodeKey(nodeID string) string {
	return fmt.Sprintf("%s:node:%s", nr.config.KeyPrefix, nodeID)
}

// nodesSetKey returns the Redis key for the set of all nodes.
func (nr *NodeRegistry) nodesSetKey() string {
	return fmt.Sprintf("%s:nodes", nr.config.KeyPrefix)
}

// schedulerNodesKey returns the Redis key for a scheduler's nodes.
func (nr *NodeRegistry) schedulerNodesKey(schedulerID string) string {
	return fmt.Sprintf("%s:scheduler:%s:nodes", nr.config.KeyPrefix, schedulerID)
}

// nodesByStatusKey returns the Redis key for nodes with a given status.
func (nr *NodeRegistry) nodesByStatusKey(status string) string {
	return fmt.Sprintf("%s:nodes:%s", nr.config.KeyPrefix, status)
}

// serializeNode converts a node to JSON with metadata.
func (nr *NodeRegistry) serializeNode(node *scheduler.Node, schedulerID string) ([]byte, error) {
	// Read-lock the node while serializing
	node.RLock()
	defer node.RUnlock()

	data := map[string]interface{}{
		"id":     node.ID,
		"name":   node.Name,
		"labels": node.Labels,
		"capacity": map[string]int64{
			"cpu_cores": node.Capacity.CPUCores,
			"memory_mb": node.Capacity.MemoryMB,
			"disk_mb":   node.Capacity.DiskMB,
		},
		"allocated": map[string]int64{
			"cpu_cores": node.Allocated.CPUCores,
			"memory_mb": node.Allocated.MemoryMB,
			"disk_mb":   node.Allocated.DiskMB,
		},
		"agents":          node.AgentIDs(),
		"status":          "ready",
		"scheduler_owner": schedulerID,
		"last_heartbeat":  time.Now().Format(time.RFC3339),
	}

	return json.Marshal(data)
}

// deserializeNode converts JSON to a node.
func (nr *NodeRegistry) deserializeNode(data []byte) (*scheduler.Node, string, error) {
	var nodeData map[string]interface{}
	if err := json.Unmarshal(data, &nodeData); err != nil {
		return nil, "", err
	}

	node := &scheduler.Node{
		ID:     nodeData["id"].(string),
		Name:   nodeData["name"].(string),
		Labels: make(map[string]string),
		Agents: make(map[api.AgentID]*scheduler.AgentAllocation),
	}

	// Parse labels
	if labels, ok := nodeData["labels"].(map[string]interface{}); ok {
		for k, v := range labels {
			if vs, ok := v.(string); ok {
				node.Labels[k] = vs
			}
		}
	}

	// Parse capacity
	if capacity, ok := nodeData["capacity"].(map[string]interface{}); ok {
		node.Capacity.CPUCores = int64(capacity["cpu_cores"].(float64))
		node.Capacity.MemoryMB = int64(capacity["memory_mb"].(float64))
		node.Capacity.DiskMB = int64(capacity["disk_mb"].(float64))
	}

	// Parse allocated
	if allocated, ok := nodeData["allocated"].(map[string]interface{}); ok {
		node.Allocated.CPUCores = int64(allocated["cpu_cores"].(float64))
		node.Allocated.MemoryMB = int64(allocated["memory_mb"].(float64))
		node.Allocated.DiskMB = int64(allocated["disk_mb"].(float64))
	}

	// Parse agents
	if agents, ok := nodeData["agents"].([]interface{}); ok {
		for _, agentID := range agents {
			if aid, ok := agentID.(string); ok {
				// Create placeholder allocation (full allocation info not stored in Redis)
				node.Agents[api.AgentID(aid)] = &scheduler.AgentAllocation{
					AgentID: api.AgentID(aid),
					NodeID:  node.ID,
					Status:  api.AgentStatusRunning,
				}
			}
		}
	}

	schedulerID := ""
	if sid, ok := nodeData["scheduler_owner"].(string); ok {
		schedulerID = sid
	}

	return node, schedulerID, nil
}
