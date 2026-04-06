package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dnakitare/aether/pkg/api"
)

func newTestStore(t *testing.T) (*PostgresStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store := &PostgresStore{
		logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		db:     db,
	}
	return store, mock
}

// =============================================================================
// CreateAgent — transactional behavior
// =============================================================================

func TestCreateAgent_PersistsWithCorrectFields(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	config := api.AgentConfig{
		ID:       "agent-1",
		TenantID: "tenant-1",
		Name:     "my-agent",
		Image:    "docker.io/python:3.11",
		Resources: api.ResourceLimits{
			CPUCount: 2,
			MemoryMB: 1024,
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO tenants").
		WithArgs("tenant-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO agents").
		WithArgs("agent-1", "tenant-1", "my-agent", "docker.io/python:3.11",
			api.AgentStatusPending, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := store.CreateAgent(ctx, config)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateAgent_RollsBackOnInsertFailure(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	config := api.AgentConfig{
		ID: "agent-dup", TenantID: "tenant-1", Name: "dup", Image: "test:1",
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO tenants").
		WithArgs("tenant-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO agents").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(fmt.Errorf("duplicate key"))
	mock.ExpectRollback()

	err := store.CreateAgent(ctx, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create agent")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateAgent_TenantUpsertAndInsertAreAtomic(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	config := api.AgentConfig{
		ID: "agent-tx", TenantID: "tenant-tx", Name: "tx-test", Image: "test:1",
	}

	// Simulate tenant upsert failure — agent insert should NOT happen
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO tenants").
		WithArgs("tenant-tx").
		WillReturnError(fmt.Errorf("connection lost"))
	mock.ExpectRollback()

	err := store.CreateAgent(ctx, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upsert tenant")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// GetAgent — deserialization and not-found behavior
// =============================================================================

func TestGetAgent_ReturnsFullInfo(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	config := api.AgentConfig{
		ID: "get-1", TenantID: "t1", Name: "getter", Image: "test:1",
		Resources: api.ResourceLimits{CPUCount: 4, MemoryMB: 2048},
	}
	configJSON, _ := json.Marshal(config)
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "name", "image", "status", "config",
		"created_at", "updated_at", "started_at", "stopped_at", "error",
	}).AddRow("get-1", "t1", "getter", "test:1", api.AgentStatusRunning,
		configJSON, now, now, now, nil, nil)

	mock.ExpectQuery("SELECT .+ FROM agents WHERE id").
		WithArgs("get-1").
		WillReturnRows(rows)

	info, err := store.GetAgent(ctx, "get-1")
	require.NoError(t, err)
	assert.Equal(t, api.AgentID("get-1"), info.Config.ID)
	assert.Equal(t, api.AgentStatusRunning, info.Status)
	assert.Equal(t, 4, info.Config.Resources.CPUCount)
	assert.NotNil(t, info.StartedAt)
}

func TestGetAgent_NotFoundReturnsError(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	mock.ExpectQuery("SELECT .+ FROM agents WHERE id").
		WithArgs("ghost").
		WillReturnError(sql.ErrNoRows)

	info, err := store.GetAgent(ctx, "ghost")
	assert.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "not found")
}

// =============================================================================
// ListAgents — tenant filtering and pagination
// =============================================================================

func TestListAgents_ReturnsByTenant(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	cfg1, _ := json.Marshal(api.AgentConfig{ID: "list-1", TenantID: "t1", Name: "a1"})
	cfg2, _ := json.Marshal(api.AgentConfig{ID: "list-2", TenantID: "t1", Name: "a2"})
	now := time.Now()

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "name", "image", "status", "config",
		"created_at", "updated_at", "started_at", "stopped_at", "error",
	}).
		AddRow("list-1", "t1", "a1", "test:1", api.AgentStatusPending, cfg1, now, now, nil, nil, nil).
		AddRow("list-2", "t1", "a2", "test:1", api.AgentStatusRunning, cfg2, now, now, now, nil, nil)

	mock.ExpectQuery("SELECT .+ FROM agents WHERE tenant_id").
		WithArgs("t1").
		WillReturnRows(rows)

	agents, err := store.ListAgents(ctx, "t1")
	require.NoError(t, err)
	assert.Len(t, agents, 2)
	assert.Equal(t, api.AgentID("list-1"), agents[0].Config.ID)
	assert.Equal(t, api.AgentID("list-2"), agents[1].Config.ID)
}

func TestListAgentsPage_ReturnsTotalCount(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	// Count query
	mock.ExpectQuery("SELECT COUNT").
		WithArgs("t1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(25))

	// Page query
	cfg, _ := json.Marshal(api.AgentConfig{ID: "page-1", TenantID: "t1"})
	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "name", "image", "status", "config",
		"created_at", "updated_at", "started_at", "stopped_at", "error",
	}).AddRow("page-1", "t1", "paged", "test:1", api.AgentStatusPending, cfg, now, now, nil, nil, nil)

	mock.ExpectQuery("SELECT .+ FROM agents WHERE tenant_id").
		WithArgs("t1", 10, 0).
		WillReturnRows(rows)

	agents, total, err := store.ListAgentsPage(ctx, "t1", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 25, total)
	assert.Len(t, agents, 1)
}

// =============================================================================
// UpdateAgentStatus — status-specific timestamp behavior
// =============================================================================

func TestUpdateAgentStatus_RunningSetStartedAt(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	mock.ExpectExec("UPDATE agents").
		WithArgs(api.AgentStatusRunning, sqlmock.AnyArg(), sqlmock.AnyArg(), "agent-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.UpdateAgentStatus(ctx, "agent-1", api.AgentStatusRunning)
	require.NoError(t, err)
}

func TestUpdateAgentStatus_StoppedSetStoppedAt(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	mock.ExpectExec("UPDATE agents").
		WithArgs(api.AgentStatusStopped, sqlmock.AnyArg(), sqlmock.AnyArg(), "agent-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.UpdateAgentStatus(ctx, "agent-1", api.AgentStatusStopped)
	require.NoError(t, err)
}

func TestUpdateAgentStatus_NotFoundReturnsError(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	mock.ExpectExec("UPDATE agents").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "ghost").
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

	err := store.UpdateAgentStatus(ctx, "ghost", api.AgentStatusRunning)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// =============================================================================
// DeleteAgent — cleanup behavior
// =============================================================================

func TestDeleteAgent_RemovesRow(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	mock.ExpectExec("DELETE FROM agents WHERE id").
		WithArgs("del-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.DeleteAgent(ctx, "del-1")
	require.NoError(t, err)
}

func TestDeleteAgent_NotFoundReturnsError(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	mock.ExpectExec("DELETE FROM agents WHERE id").
		WithArgs("ghost").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := store.DeleteAgent(ctx, "ghost")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// =============================================================================
// SetAgentError — marks agent as failed with message
// =============================================================================

func TestSetAgentError_SetsFailedStatus(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	mock.ExpectExec("UPDATE agents").
		WithArgs(api.AgentStatusFailed, "out of memory", sqlmock.AnyArg(), sqlmock.AnyArg(), "err-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.SetAgentError(ctx, "err-1", "out of memory")
	require.NoError(t, err)
}

// =============================================================================
// GetTenantAgentCount — counting behavior
// =============================================================================

func TestGetTenantAgentCount_ReturnsCorrectCount(t *testing.T) {
	store, mock := newTestStore(t)
	ctx := context.Background()

	mock.ExpectQuery("SELECT COUNT").
		WithArgs("t1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	count, err := store.GetTenantAgentCount(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, 7, count)
}
