package routing_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/dnakitare/aether/internal/routing"
	"github.com/dnakitare/aether/pkg/api"
)

func TestRouterConfig(t *testing.T) {
	config := routing.DefaultRouterConfig()

	if config.HealthCheckInterval == 0 {
		t.Error("Expected HealthCheckInterval to be set")
	}

	if config.HealthCheckTimeout == 0 {
		t.Error("Expected HealthCheckTimeout to be set")
	}

	if config.UnhealthyThreshold == 0 {
		t.Error("Expected UnhealthyThreshold to be set")
	}
}

func TestRouter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := routing.DefaultRouterConfig()
	router := routing.NewRouter(logger, config)

	ctx := context.Background()

	// Test service registration
	agentID := api.AgentID("test-agent")
	tenantID := api.TenantID("test-tenant")
	address := "192.168.1.100"
	port := 8080
	metadata := map[string]string{"version": "1.0"}

	err := router.Register(ctx, agentID, tenantID, address, port, metadata)
	if err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}

	// Test service lookup
	service, err := router.Lookup(agentID)
	if err != nil {
		t.Fatalf("Failed to lookup service: %v", err)
	}

	if service.AgentID != agentID {
		t.Errorf("Expected AgentID = %s, got %s", agentID, service.AgentID)
	}

	if service.Address != address {
		t.Errorf("Expected Address = %s, got %s", address, service.Address)
	}

	// Test list services
	services := router.ListServices()
	if len(services) != 1 {
		t.Errorf("Expected 1 service, got %d", len(services))
	}

	// Test deregister
	err = router.Deregister(ctx, agentID)
	if err != nil {
		t.Fatalf("Failed to deregister service: %v", err)
	}

	// Verify deregistered
	_, err = router.Lookup(agentID)
	if err == nil {
		t.Error("Expected error when looking up deregistered service")
	}
}

func TestHealthStatus(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := routing.DefaultRouterConfig()
	router := routing.NewRouter(logger, config)

	ctx := context.Background()

	agentID := api.AgentID("test-agent")
	tenantID := api.TenantID("test-tenant")

	router.Register(ctx, agentID, tenantID, "localhost", 8080, nil)

	// Mark as healthy
	router.UpdateHealth(agentID, true)

	service, _ := router.Lookup(agentID)
	if service.Health != routing.HealthHealthy {
		t.Errorf("Expected health = healthy, got %s", service.Health)
	}

	// Mark as unhealthy
	for i := 0; i < config.UnhealthyThreshold; i++ {
		router.UpdateHealth(agentID, false)
	}

	service, _ = router.Lookup(agentID)
	if service.Health != routing.HealthUnhealthy {
		t.Errorf("Expected health = unhealthy, got %s", service.Health)
	}
}

func TestLoadBalancer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := routing.DefaultRouterConfig()
	router := routing.NewRouter(logger, config)

	ctx := context.Background()
	tenantID := api.TenantID("test-tenant")

	// Register multiple agents
	for i := 1; i <= 3; i++ {
		agentID := api.AgentID("agent-" + string(rune('0'+i)))
		router.Register(ctx, agentID, tenantID, "localhost", 8080+i, nil)
		router.UpdateHealth(agentID, true)
	}

	// Test load balancer
	lb := routing.NewLoadBalancer(logger, router, routing.StrategyRoundRobin)

	agent, err := lb.SelectAgent(tenantID)
	if err != nil {
		t.Fatalf("Failed to select agent: %v", err)
	}

	if agent == nil {
		t.Error("Expected non-nil agent")
	}
}

func TestTrafficPolicy(t *testing.T) {
	policy := routing.DefaultTrafficPolicy()

	if policy.MaxRetries == 0 {
		t.Error("Expected MaxRetries to be set")
	}

	if policy.ConnectionTimeout == 0 {
		t.Error("Expected ConnectionTimeout to be set")
	}

	if policy.FailureThreshold == 0 {
		t.Error("Expected FailureThreshold to be set")
	}
}

func TestCircuitBreaker(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	policy := routing.DefaultTrafficPolicy()
	cb := routing.NewCircuitBreaker(logger, policy)

	// Initial state should be closed
	if cb.GetState() != routing.StateClosed {
		t.Errorf("Expected initial state = closed, got %s", cb.GetState())
	}

	// Should allow requests when closed
	if err := cb.Allow(); err != nil {
		t.Error("Expected requests to be allowed when circuit is closed")
	}

	// Record failures to open circuit
	for i := 0; i < policy.FailureThreshold; i++ {
		cb.RecordFailure()
	}

	if cb.GetState() != routing.StateOpen {
		t.Errorf("Expected state = open after failures, got %s", cb.GetState())
	}

	// Should reject requests when open
	if err := cb.Allow(); err == nil {
		t.Error("Expected requests to be rejected when circuit is open")
	}
}

func TestConnectionPool(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := routing.DefaultConnectionPoolConfig()
	pool := routing.NewConnectionPool(logger, config)

	agentID := api.AgentID("test-agent")

	// Get connection
	err := pool.GetConnection(agentID)
	if err != nil {
		t.Fatalf("Failed to get connection: %v", err)
	}

	// Release connection
	pool.ReleaseConnection(agentID)

	// Get multiple connections
	for i := 0; i < 5; i++ {
		err := pool.GetConnection(agentID)
		if err != nil {
			t.Fatalf("Failed to get connection %d: %v", i, err)
		}
	}
}
