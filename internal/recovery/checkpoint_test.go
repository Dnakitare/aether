package recovery_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dnakitare/aether/internal/recovery"
	"github.com/dnakitare/aether/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"log/slog"
	"os"
)

// newTestLogger returns a silent logger suitable for use in tests.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

// newMockDB creates a sql.DB backed by sqlmock and returns it along with
// the Sqlmock controller. The caller must call mock.ExpectationsWereMet()
// at the end of each test.
func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, mock
}

// expectTableCreation sets up the single Exec expectation that
// NewCheckpointManager fires when calling createTable.
func expectTableCreation(mock sqlmock.Sqlmock) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS checkpoints").
		WillReturnResult(sqlmock.NewResult(0, 0))
}

// mustMarshal serializes v to JSON bytes, panicking on error (test helper only).
func mustMarshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshal: %v", err))
	}
	return b
}

// ---------------------------------------------------------------------------
// NewCheckpointManager
// ---------------------------------------------------------------------------

func TestNewCheckpointManager_CreatesTable(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)

	require.NoError(t, err)
	assert.NotNil(t, cm)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNewCheckpointManager_ReturnsErrorWhenTableCreationFails(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS checkpoints").
		WillReturnError(fmt.Errorf("permission denied"))

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)

	require.Error(t, err)
	assert.Nil(t, cm)
	assert.Contains(t, err.Error(), "failed to create checkpoints table")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ---------------------------------------------------------------------------
// CreateCheckpoint
// ---------------------------------------------------------------------------

func TestCreateCheckpoint_Success(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	agentID := api.AgentID("agent-abc")
	tenantID := api.TenantID("tenant-xyz")
	state := map[string]interface{}{"step": "running", "progress": 42}
	metadata := map[string]string{"source": "scheduler"}
	createdAt := time.Now().UTC()

	// Transaction sequence
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0))
	mock.ExpectQuery("INSERT INTO checkpoints").
		WithArgs(
			sqlmock.AnyArg(), // agent_id
			sqlmock.AnyArg(), // tenant_id
			1,                // version (0+1)
			sqlmock.AnyArg(), // state JSON
			sqlmock.AnyArg(), // metadata JSON
			sqlmock.AnyArg(), // size
			sqlmock.AnyArg(), // compressed
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(1), createdAt))
	mock.ExpectExec("DELETE FROM checkpoints").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	cp, err := cm.CreateCheckpoint(context.Background(), agentID, tenantID, state, metadata)

	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, int64(1), cp.ID)
	assert.Equal(t, agentID, cp.AgentID)
	assert.Equal(t, tenantID, cp.TenantID)
	assert.Equal(t, 1, cp.Version)
	assert.Equal(t, "running", cp.State["step"])
	assert.Equal(t, "scheduler", cp.Metadata["source"])
	assert.Equal(t, createdAt.Unix(), cp.CreatedAt.Unix())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateCheckpoint_ExceedsMaxSize(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	cfg := recovery.DefaultCheckpointConfig()
	cfg.MaxCheckpointSize = 10 // intentionally tiny
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	// A state that serializes to well over 10 bytes.
	bigState := map[string]interface{}{
		"payload": strings.Repeat("x", 1000),
	}

	cp, err := cm.CreateCheckpoint(
		context.Background(),
		api.AgentID("agent-1"),
		api.TenantID("tenant-1"),
		bigState,
		nil,
	)

	require.Error(t, err)
	assert.Nil(t, cp)
	assert.Contains(t, err.Error(), "exceeds maximum")

	// No DB calls should have been made beyond table creation.
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateCheckpoint_VersionAutoIncrement(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	agentID := api.AgentID("agent-versioned")
	tenantID := api.TenantID("tenant-1")
	state := map[string]interface{}{"status": "ok"}
	createdAt := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	// Existing max version is 5; new checkpoint should be version 6.
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(5))
	mock.ExpectQuery("INSERT INTO checkpoints").
		WithArgs(
			sqlmock.AnyArg(), // agent_id
			sqlmock.AnyArg(), // tenant_id
			6,                // version must be 6
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(10), createdAt))
	mock.ExpectExec("DELETE FROM checkpoints").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	cp, err := cm.CreateCheckpoint(context.Background(), agentID, tenantID, state, nil)

	require.NoError(t, err)
	assert.Equal(t, 6, cp.Version)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateCheckpoint_RollsBackOnInsertError(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(0))
	mock.ExpectQuery("INSERT INTO checkpoints").
		WithArgs(
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnError(fmt.Errorf("unique constraint violation"))
	mock.ExpectRollback()

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	cp, err := cm.CreateCheckpoint(
		context.Background(),
		api.AgentID("agent-1"),
		api.TenantID("tenant-1"),
		map[string]interface{}{"k": "v"},
		nil,
	)

	require.Error(t, err)
	assert.Nil(t, cp)
	assert.Contains(t, err.Error(), "failed to insert checkpoint")
}

// ---------------------------------------------------------------------------
// GetLatestCheckpoint
// ---------------------------------------------------------------------------

func TestGetLatestCheckpoint_Found(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	agentID := api.AgentID("agent-latest")
	tenantID := api.TenantID("tenant-1")
	createdAt := time.Now().UTC()
	stateJSON := mustMarshal(map[string]interface{}{"mode": "active"})
	metadataJSON := mustMarshal(map[string]string{"env": "prod"})

	mock.ExpectQuery("SELECT id, agent_id, tenant_id, version, state, metadata, created_at, size").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "agent_id", "tenant_id", "version", "state", "metadata", "created_at", "size"},
		).AddRow(int64(7), string(agentID), string(tenantID), 3, stateJSON, metadataJSON, createdAt, int64(len(stateJSON))))

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	cp, err := cm.GetLatestCheckpoint(context.Background(), agentID)

	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, int64(7), cp.ID)
	assert.Equal(t, agentID, cp.AgentID)
	assert.Equal(t, tenantID, cp.TenantID)
	assert.Equal(t, 3, cp.Version)
	assert.Equal(t, "active", cp.State["mode"])
	assert.Equal(t, "prod", cp.Metadata["env"])
	assert.Equal(t, createdAt.Unix(), cp.CreatedAt.Unix())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetLatestCheckpoint_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	mock.ExpectQuery("SELECT id, agent_id, tenant_id, version, state, metadata, created_at, size").
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	cp, err := cm.GetLatestCheckpoint(context.Background(), api.AgentID("ghost-agent"))

	require.Error(t, err)
	assert.Nil(t, cp)
	assert.Contains(t, err.Error(), "no checkpoint found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ---------------------------------------------------------------------------
// GetCheckpointByVersion
// ---------------------------------------------------------------------------

func TestGetCheckpointByVersion_Found(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	agentID := api.AgentID("agent-versioned")
	tenantID := api.TenantID("tenant-1")
	createdAt := time.Now().UTC()
	stateJSON := mustMarshal(map[string]interface{}{"step": "done"})
	metadataJSON := mustMarshal(map[string]string{"tag": "v3"})

	mock.ExpectQuery("SELECT id, agent_id, tenant_id, version, state, metadata, created_at, size").
		WithArgs(sqlmock.AnyArg(), 3).
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "agent_id", "tenant_id", "version", "state", "metadata", "created_at", "size"},
		).AddRow(int64(3), string(agentID), string(tenantID), 3, stateJSON, metadataJSON, createdAt, int64(len(stateJSON))))

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	cp, err := cm.GetCheckpointByVersion(context.Background(), agentID, 3)

	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, 3, cp.Version)
	assert.Equal(t, "done", cp.State["step"])
	assert.Equal(t, "v3", cp.Metadata["tag"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetCheckpointByVersion_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	mock.ExpectQuery("SELECT id, agent_id, tenant_id, version, state, metadata, created_at, size").
		WithArgs(sqlmock.AnyArg(), 99).
		WillReturnError(sql.ErrNoRows)

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	cp, err := cm.GetCheckpointByVersion(context.Background(), api.AgentID("agent-1"), 99)

	require.Error(t, err)
	assert.Nil(t, cp)
	assert.Contains(t, err.Error(), "checkpoint version 99 not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ---------------------------------------------------------------------------
// ListCheckpoints
// ---------------------------------------------------------------------------

func TestListCheckpoints_Multiple(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	agentID := api.AgentID("agent-multi")
	tenantID := api.TenantID("tenant-1")
	now := time.Now().UTC()

	rows := sqlmock.NewRows([]string{"id", "agent_id", "tenant_id", "version", "created_at", "size"}).
		AddRow(int64(3), string(agentID), string(tenantID), 3, now, int64(100)).
		AddRow(int64(2), string(agentID), string(tenantID), 2, now, int64(80)).
		AddRow(int64(1), string(agentID), string(tenantID), 1, now, int64(60))

	mock.ExpectQuery("SELECT id, agent_id, tenant_id, version, created_at, size").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	checkpoints, err := cm.ListCheckpoints(context.Background(), agentID)

	require.NoError(t, err)
	require.Len(t, checkpoints, 3)
	assert.Equal(t, 3, checkpoints[0].Version)
	assert.Equal(t, 2, checkpoints[1].Version)
	assert.Equal(t, 1, checkpoints[2].Version)
	assert.Equal(t, agentID, checkpoints[0].AgentID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListCheckpoints_Empty(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	mock.ExpectQuery("SELECT id, agent_id, tenant_id, version, created_at, size").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "tenant_id", "version", "created_at", "size"}))

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	checkpoints, err := cm.ListCheckpoints(context.Background(), api.AgentID("agent-empty"))

	require.NoError(t, err)
	assert.Empty(t, checkpoints)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ---------------------------------------------------------------------------
// DeleteCheckpoint
// ---------------------------------------------------------------------------

func TestDeleteCheckpoint_Success(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	mock.ExpectExec("DELETE FROM checkpoints WHERE agent_id").
		WithArgs(sqlmock.AnyArg(), 2).
		WillReturnResult(sqlmock.NewResult(0, 1)) // 1 row affected

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	err = cm.DeleteCheckpoint(context.Background(), api.AgentID("agent-del"), 2)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteCheckpoint_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	expectTableCreation(mock)

	mock.ExpectExec("DELETE FROM checkpoints WHERE agent_id").
		WithArgs(sqlmock.AnyArg(), 99).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

	cfg := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(newTestLogger(), db, cfg)
	require.NoError(t, err)

	err = cm.DeleteCheckpoint(context.Background(), api.AgentID("agent-del"), 99)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "checkpoint not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}
