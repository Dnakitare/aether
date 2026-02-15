package state

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aether-runtime/aether/pkg/api"
)

// setupTestDB creates a test database connection.
// Skips tests if PostgreSQL is not available.
func setupTestDB(t *testing.T) *PostgresStore {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping PostgreSQL test in short mode")
	}

	// Test database DSN (same as used in integration tests)
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://aether:aether_test_password@localhost:5433/aether_test?sslmode=disable"
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := PostgresConfig{
		DSN:             dsn,
		MaxOpenConns:    10,
		MaxIdleConns:    2,
		ConnMaxLifetime: 5 * time.Minute,
	}

	store, err := NewPostgresStore(logger, config)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}

	// Clean up test data
	cleanupTestData(t, store.db)

	t.Cleanup(func() {
		cleanupTestData(t, store.db)
		store.Close()
	})

	return store
}

// cleanupTestData removes test data from database.
func cleanupTestData(t *testing.T, db *sql.DB) {
	t.Helper()

	// Delete test agents (cascade will handle related records)
	_, err := db.Exec("DELETE FROM agents WHERE tenant_id LIKE 'test-%'")
	if err != nil {
		t.Logf("Warning: failed to clean up test agents: %v", err)
	}

	// Delete test tenants if they exist
	_, err = db.Exec("DELETE FROM tenants WHERE id LIKE 'test-%'")
	if err != nil {
		t.Logf("Warning: failed to clean up test tenants: %v", err)
	}
}

// createTestTenant creates a test tenant for agent tests.
func createTestTenant(t *testing.T, db *sql.DB, tenantID api.TenantID) {
	t.Helper()

	query := `
		INSERT INTO tenants (id, name, tier, created_at, updated_at)
		VALUES ($1, $2, 'test', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`
	_, err := db.Exec(query, tenantID, "Test Tenant")
	require.NoError(t, err, "failed to create test tenant")
}

func TestNewPostgresStore(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping PostgreSQL test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("successful connection", func(t *testing.T) {
		config := PostgresConfig{
			DSN: "postgres://aether:aether_test_password@localhost:5433/aether_test?sslmode=disable",
		}

		store, err := NewPostgresStore(logger, config)
		if err != nil {
			t.Skipf("PostgreSQL not available: %v", err)
		}
		defer store.Close()

		assert.NotNil(t, store)
		assert.NotNil(t, store.db)
	})

	t.Run("missing DSN", func(t *testing.T) {
		config := PostgresConfig{}

		store, err := NewPostgresStore(logger, config)
		assert.Error(t, err)
		assert.Nil(t, store)
		assert.Contains(t, err.Error(), "DSN is required")
	})

	t.Run("invalid DSN", func(t *testing.T) {
		config := PostgresConfig{
			DSN: "postgres://invalid:invalid@localhost:9999/invalid",
		}

		store, err := NewPostgresStore(logger, config)
		assert.Error(t, err)
		assert.Nil(t, store)
	})
}

func TestCreateAgent(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	tenantID := api.TenantID("test-tenant-create")
	createTestTenant(t, store.db, tenantID)

	t.Run("create agent successfully", func(t *testing.T) {
		config := api.AgentConfig{
			ID:       api.AgentID("agent-1"),
			TenantID: tenantID,
			Name:     "test-agent-1",
			Image:    "test-image:latest",
			Resources: api.ResourceLimits{
				CPUCount: 2,
				MemoryMB: 1024,
				DiskMB:   10240,
			},
			Env: map[string]string{
				"TEST_VAR": "test-value",
			},
			Labels: map[string]string{
				"env": "test",
			},
		}

		err := store.CreateAgent(ctx, config)
		require.NoError(t, err)

		// Verify it was created
		agent, err := store.GetAgent(ctx, config.ID)
		require.NoError(t, err)
		assert.Equal(t, config.ID, agent.Config.ID)
		assert.Equal(t, config.Name, agent.Config.Name)
		assert.Equal(t, api.AgentStatusPending, agent.Status)
		assert.Equal(t, "test-value", agent.Config.Env["TEST_VAR"])
		assert.Equal(t, "test", agent.Config.Labels["env"])
	})

	t.Run("duplicate agent ID", func(t *testing.T) {
		config := api.AgentConfig{
			ID:       api.AgentID("agent-duplicate"),
			TenantID: tenantID,
			Name:     "duplicate",
			Image:    "test:latest",
			Resources: api.ResourceLimits{
				CPUCount: 1,
				MemoryMB: 512,
			},
		}

		// First creation should succeed
		err := store.CreateAgent(ctx, config)
		require.NoError(t, err)

		// Second creation should fail
		err = store.CreateAgent(ctx, config)
		assert.Error(t, err)
	})
}

func TestGetAgent(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	tenantID := api.TenantID("test-tenant-get")
	createTestTenant(t, store.db, tenantID)

	t.Run("get existing agent", func(t *testing.T) {
		config := api.AgentConfig{
			ID:       api.AgentID("agent-get-1"),
			TenantID: tenantID,
			Name:     "get-test",
			Image:    "test:latest",
			Resources: api.ResourceLimits{
				CPUCount: 2,
				MemoryMB: 2048,
			},
		}

		err := store.CreateAgent(ctx, config)
		require.NoError(t, err)

		agent, err := store.GetAgent(ctx, config.ID)
		require.NoError(t, err)
		assert.Equal(t, config.ID, agent.Config.ID)
		assert.Equal(t, config.Name, agent.Config.Name)
		assert.Equal(t, config.TenantID, agent.Config.TenantID)
		assert.Equal(t, api.AgentStatusPending, agent.Status)
		assert.NotZero(t, agent.CreatedAt)
		assert.Nil(t, agent.StartedAt)
		assert.Nil(t, agent.StoppedAt)
	})

	t.Run("get non-existent agent", func(t *testing.T) {
		agent, err := store.GetAgent(ctx, "non-existent-agent")
		assert.Error(t, err)
		assert.Nil(t, agent)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestListAgents(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	tenantID := api.TenantID("test-tenant-list")
	createTestTenant(t, store.db, tenantID)

	t.Run("list multiple agents", func(t *testing.T) {
		// Create multiple agents
		for i := 1; i <= 3; i++ {
			config := api.AgentConfig{
				ID:       api.AgentID(string(rune('a' + i))),
				TenantID: tenantID,
				Name:     string(rune('a' + i)),
				Image:    "test:latest",
				Resources: api.ResourceLimits{
					CPUCount: 1,
					MemoryMB: 512,
				},
			}
			err := store.CreateAgent(ctx, config)
			require.NoError(t, err)
		}

		agents, err := store.ListAgents(ctx, tenantID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(agents), 3)

		// Verify they're sorted by created_at DESC
		for i := 0; i < len(agents)-1; i++ {
			assert.True(t, agents[i].CreatedAt.After(agents[i+1].CreatedAt) ||
				agents[i].CreatedAt.Equal(agents[i+1].CreatedAt))
		}
	})

	t.Run("list empty tenant", func(t *testing.T) {
		emptyTenant := api.TenantID("test-tenant-empty")
		createTestTenant(t, store.db, emptyTenant)

		agents, err := store.ListAgents(ctx, emptyTenant)
		require.NoError(t, err)
		assert.Empty(t, agents)
	})
}

func TestUpdateAgentStatus(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	tenantID := api.TenantID("test-tenant-update")
	createTestTenant(t, store.db, tenantID)

	config := api.AgentConfig{
		ID:       api.AgentID("agent-update-status"),
		TenantID: tenantID,
		Name:     "status-test",
		Image:    "test:latest",
		Resources: api.ResourceLimits{
			CPUCount: 1,
			MemoryMB: 512,
		},
	}

	err := store.CreateAgent(ctx, config)
	require.NoError(t, err)

	t.Run("update to running", func(t *testing.T) {
		err := store.UpdateAgentStatus(ctx, config.ID, api.AgentStatusRunning)
		require.NoError(t, err)

		agent, err := store.GetAgent(ctx, config.ID)
		require.NoError(t, err)
		assert.Equal(t, api.AgentStatusRunning, agent.Status)
		assert.NotNil(t, agent.StartedAt)
	})

	t.Run("update to stopped", func(t *testing.T) {
		err := store.UpdateAgentStatus(ctx, config.ID, api.AgentStatusStopped)
		require.NoError(t, err)

		agent, err := store.GetAgent(ctx, config.ID)
		require.NoError(t, err)
		assert.Equal(t, api.AgentStatusStopped, agent.Status)
		assert.NotNil(t, agent.StoppedAt)
	})

	t.Run("update non-existent agent", func(t *testing.T) {
		err := store.UpdateAgentStatus(ctx, "non-existent", api.AgentStatusRunning)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestSetAgentError(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	tenantID := api.TenantID("test-tenant-error")
	createTestTenant(t, store.db, tenantID)

	config := api.AgentConfig{
		ID:       api.AgentID("agent-error"),
		TenantID: tenantID,
		Name:     "error-test",
		Image:    "test:latest",
		Resources: api.ResourceLimits{
			CPUCount: 1,
			MemoryMB: 512,
		},
	}

	err := store.CreateAgent(ctx, config)
	require.NoError(t, err)

	t.Run("set error message", func(t *testing.T) {
		errorMsg := "VM failed to start"
		err := store.SetAgentError(ctx, config.ID, errorMsg)
		require.NoError(t, err)

		agent, err := store.GetAgent(ctx, config.ID)
		require.NoError(t, err)
		assert.Equal(t, api.AgentStatusFailed, agent.Status)
		assert.Equal(t, errorMsg, agent.Error)
		assert.NotNil(t, agent.StoppedAt)
	})
}

func TestDeleteAgent(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	tenantID := api.TenantID("test-tenant-delete")
	createTestTenant(t, store.db, tenantID)

	t.Run("delete existing agent", func(t *testing.T) {
		config := api.AgentConfig{
			ID:       api.AgentID("agent-delete-1"),
			TenantID: tenantID,
			Name:     "delete-test",
			Image:    "test:latest",
			Resources: api.ResourceLimits{
				CPUCount: 1,
				MemoryMB: 512,
			},
		}

		err := store.CreateAgent(ctx, config)
		require.NoError(t, err)

		// Verify it exists
		_, err = store.GetAgent(ctx, config.ID)
		require.NoError(t, err)

		// Delete it
		err = store.DeleteAgent(ctx, config.ID)
		require.NoError(t, err)

		// Verify it's gone
		_, err = store.GetAgent(ctx, config.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("delete non-existent agent", func(t *testing.T) {
		err := store.DeleteAgent(ctx, "non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetAgentsByStatus(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	tenantID := api.TenantID("test-tenant-status-filter")
	createTestTenant(t, store.db, tenantID)

	// Create agents with different statuses
	statuses := []api.AgentStatus{
		api.AgentStatusPending,
		api.AgentStatusRunning,
		api.AgentStatusStopped,
	}

	for i, status := range statuses {
		config := api.AgentConfig{
			ID:       api.AgentID(string(rune('x' + i))),
			TenantID: tenantID,
			Name:     string(rune('x' + i)),
			Image:    "test:latest",
			Resources: api.ResourceLimits{
				CPUCount: 1,
				MemoryMB: 512,
			},
		}
		err := store.CreateAgent(ctx, config)
		require.NoError(t, err)

		if status != api.AgentStatusPending {
			err = store.UpdateAgentStatus(ctx, config.ID, status)
			require.NoError(t, err)
		}
	}

	t.Run("get pending agents", func(t *testing.T) {
		agents, err := store.GetAgentsByStatus(ctx, api.AgentStatusPending)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(agents), 1)
		for _, agent := range agents {
			assert.Equal(t, api.AgentStatusPending, agent.Status)
		}
	})

	t.Run("get running agents", func(t *testing.T) {
		agents, err := store.GetAgentsByStatus(ctx, api.AgentStatusRunning)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(agents), 1)
		for _, agent := range agents {
			assert.Equal(t, api.AgentStatusRunning, agent.Status)
		}
	})
}

func TestGetTenantAgentCount(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()

	tenantID := api.TenantID("test-tenant-count")
	createTestTenant(t, store.db, tenantID)

	t.Run("count agents", func(t *testing.T) {
		// Create 5 agents
		for i := 1; i <= 5; i++ {
			config := api.AgentConfig{
				ID:       api.AgentID(string(rune('m' + i))),
				TenantID: tenantID,
				Name:     string(rune('m' + i)),
				Image:    "test:latest",
				Resources: api.ResourceLimits{
					CPUCount: 1,
					MemoryMB: 512,
				},
			}
			err := store.CreateAgent(ctx, config)
			require.NoError(t, err)
		}

		count, err := store.GetTenantAgentCount(ctx, tenantID)
		require.NoError(t, err)
		assert.Equal(t, 5, count)
	})

	t.Run("count empty tenant", func(t *testing.T) {
		emptyTenant := api.TenantID("test-tenant-count-empty")
		createTestTenant(t, store.db, emptyTenant)

		count, err := store.GetTenantAgentCount(ctx, emptyTenant)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}
