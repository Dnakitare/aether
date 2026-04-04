package audit

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dnakitare/aether/pkg/api"
)

func TestNewLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("successful creation", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		// Expect ping
		mock.ExpectPing()

		// Expect schema init
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS audit_logs").
			WillReturnResult(sqlmock.NewResult(0, 0))

		config := Config{
			DSN:       "test-dsn",
			TableName: "audit_logs",
		}

		// Create a wrapper that returns our mock db
		al := &Logger{
			logger: logger,
			db:     db,
			config: config,
		}

		assert.NotNil(t, al)
		assert.Equal(t, "audit_logs", al.config.TableName)
	})

	t.Run("missing DSN", func(t *testing.T) {
		config := Config{}

		al, err := NewLogger(logger, config)
		assert.Error(t, err)
		assert.Nil(t, al)
		assert.Contains(t, err.Error(), "DSN is required")
	})

	t.Run("default table name", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectPing()
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS audit_logs").
			WillReturnResult(sqlmock.NewResult(0, 0))

		config := Config{
			DSN: "test-dsn",
			// TableName not specified
		}

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{DSN: config.DSN, TableName: "audit_logs"},
		}

		assert.Equal(t, "audit_logs", al.config.TableName)
	})
}

func TestLog(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("log event successfully", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{TableName: "audit_logs"},
		}

		event := &Event{
			TenantID:   "test-tenant",
			UserID:     "test-user",
			Action:     ActionCreate,
			Resource:   ResourceAgent,
			ResourceID: "agent-123",
			Result:     ResultSuccess,
			IPAddress:  "192.168.1.1",
			UserAgent:  "test-agent",
			Details: map[string]interface{}{
				"image": "python:3.11",
			},
		}

		// Expect INSERT query
		mock.ExpectQuery("INSERT INTO audit_logs").
			WithArgs(
				sqlmock.AnyArg(), // timestamp
				event.TenantID,
				event.UserID,
				event.Action,
				event.Resource,
				event.ResourceID,
				event.Result,
				event.IPAddress,
				event.UserAgent,
				sqlmock.AnyArg(), // details JSON
				"",               // no error
			).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

		err = al.Log(context.Background(), event)
		require.NoError(t, err)
		assert.Equal(t, int64(1), event.ID)
		assert.False(t, event.Timestamp.IsZero())

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})

	t.Run("log event with error", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{TableName: "audit_logs"},
		}

		event := &Event{
			TenantID:   "test-tenant",
			UserID:     "test-user",
			Action:     ActionCreate,
			Resource:   ResourceAgent,
			ResourceID: "agent-123",
			Result:     ResultFailure,
			Error:      "quota exceeded",
		}

		mock.ExpectQuery("INSERT INTO audit_logs").
			WithArgs(
				sqlmock.AnyArg(),
				event.TenantID,
				event.UserID,
				event.Action,
				event.Resource,
				event.ResourceID,
				event.Result,
				"",
				"",
				sqlmock.AnyArg(), // details can be nil or empty JSON
				event.Error,
			).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))

		err = al.Log(context.Background(), event)
		require.NoError(t, err)
		assert.Equal(t, int64(2), event.ID)

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})

	t.Run("database insert fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{TableName: "audit_logs"},
		}

		event := &Event{
			TenantID: "test-tenant",
			UserID:   "test-user",
			Action:   ActionCreate,
			Resource: ResourceAgent,
			Result:   ResultSuccess,
		}

		mock.ExpectQuery("INSERT INTO audit_logs").
			WillReturnError(errors.New("database error"))

		err = al.Log(context.Background(), event)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to insert audit log")

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})

	t.Run("auto-populate timestamp", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{TableName: "audit_logs"},
		}

		event := &Event{
			TenantID: "test-tenant",
			UserID:   "test-user",
			Action:   ActionLogin,
			Resource: ResourceAgent,
			Result:   ResultSuccess,
			// Timestamp not set
		}

		mock.ExpectQuery("INSERT INTO audit_logs").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))

		err = al.Log(context.Background(), event)
		require.NoError(t, err)
		assert.False(t, event.Timestamp.IsZero(), "timestamp should be auto-populated")

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})
}

func TestQuery(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("query with filters", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{TableName: "audit_logs"},
		}

		filters := QueryFilters{
			TenantID:  "test-tenant",
			UserID:    "test-user",
			Action:    ActionCreate,
			StartTime: time.Now().Add(-24 * time.Hour),
			EndTime:   time.Now(),
			Limit:     10,
			Offset:    5,
		}

		rows := sqlmock.NewRows([]string{
			"id", "timestamp", "tenant_id", "user_id", "action", "resource",
			"resource_id", "result", "ip_address", "user_agent", "details", "error",
		}).AddRow(
			1,
			time.Now(),
			"test-tenant",
			"test-user",
			"create",
			"agent",
			"agent-1",
			"success",
			"192.168.1.1",
			"test-agent",
			[]byte(`{"image":"python:3.11"}`),
			"",
		)

		mock.ExpectQuery("SELECT .* FROM audit_logs").
			WithArgs(
				filters.TenantID,
				filters.UserID,
				filters.Action,
				filters.StartTime,
				filters.EndTime,
				filters.Limit,
				filters.Offset,
			).
			WillReturnRows(rows)

		events, err := al.Query(context.Background(), filters)
		require.NoError(t, err)
		assert.Len(t, events, 1)
		assert.Equal(t, api.TenantID("test-tenant"), events[0].TenantID)
		assert.Equal(t, ActionCreate, events[0].Action)

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})

	t.Run("query with pagination", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{TableName: "audit_logs"},
		}

		filters := QueryFilters{
			Limit:  20,
			Offset: 40,
		}

		mock.ExpectQuery("SELECT .* FROM audit_logs").
			WithArgs(filters.Limit, filters.Offset).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "timestamp", "tenant_id", "user_id", "action", "resource",
				"resource_id", "result", "ip_address", "user_agent", "details", "error",
			}))

		events, err := al.Query(context.Background(), filters)
		require.NoError(t, err)
		assert.NotNil(t, events)

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})

	t.Run("database query fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{TableName: "audit_logs"},
		}

		filters := QueryFilters{TenantID: "test-tenant"}

		mock.ExpectQuery("SELECT .* FROM audit_logs").
			WillReturnError(errors.New("query error"))

		events, err := al.Query(context.Background(), filters)
		assert.Error(t, err)
		assert.Nil(t, events)
		assert.Contains(t, err.Error(), "failed to query audit logs")

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})
}

func TestCleanupOld(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("cleanup old logs successfully", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{
				TableName:     "audit_logs",
				RetentionDays: 90,
			},
		}

		mock.ExpectExec("DELETE FROM audit_logs").
			WithArgs(sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 150))

		deleted, err := al.CleanupOld(context.Background())
		require.NoError(t, err)
		assert.Equal(t, int64(150), deleted)

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})

	t.Run("zero retention means no cleanup", func(t *testing.T) {
		db, _, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{
				TableName:     "audit_logs",
				RetentionDays: 0, // Never delete
			},
		}

		deleted, err := al.CleanupOld(context.Background())
		require.NoError(t, err)
		assert.Equal(t, int64(0), deleted)
		// No expectations - should not execute DELETE
	})

	t.Run("cleanup fails", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{
				TableName:     "audit_logs",
				RetentionDays: 30,
			},
		}

		mock.ExpectExec("DELETE FROM audit_logs").
			WillReturnError(errors.New("cleanup error"))

		_, err = al.CleanupOld(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to cleanup old logs")

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})
}

func TestClose(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("close successfully", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)

		al := &Logger{
			logger: logger,
			db:     db,
			config: Config{TableName: "audit_logs"},
		}

		mock.ExpectClose()

		err = al.Close()
		require.NoError(t, err)

		err = mock.ExpectationsWereMet()
		assert.NoError(t, err)
	})
}

// Test audit action and result constants
func TestConstants(t *testing.T) {
	t.Run("action constants", func(t *testing.T) {
		assert.Equal(t, Action("create"), ActionCreate)
		assert.Equal(t, Action("read"), ActionRead)
		assert.Equal(t, Action("update"), ActionUpdate)
		assert.Equal(t, Action("delete"), ActionDelete)
		assert.Equal(t, Action("start"), ActionStart)
		assert.Equal(t, Action("stop"), ActionStop)
		assert.Equal(t, Action("login"), ActionLogin)
		assert.Equal(t, Action("logout"), ActionLogout)
	})

	t.Run("resource type constants", func(t *testing.T) {
		assert.Equal(t, ResourceType("agent"), ResourceAgent)
		assert.Equal(t, ResourceType("quota"), ResourceQuota)
		assert.Equal(t, ResourceType("policy"), ResourcePolicy)
		assert.Equal(t, ResourceType("secret"), ResourceSecret)
		assert.Equal(t, ResourceType("api_key"), ResourceAPIKey)
		assert.Equal(t, ResourceType("firewall"), ResourceFirewall)
	})

	t.Run("result constants", func(t *testing.T) {
		assert.Equal(t, Result("success"), ResultSuccess)
		assert.Equal(t, Result("failure"), ResultFailure)
		assert.Equal(t, Result("denied"), ResultDenied)
	})
}

// Benchmark audit logging performance
func BenchmarkLog(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	db, mock, _ := sqlmock.New()
	defer db.Close()

	al := &Logger{
		logger: logger,
		db:     db,
		config: Config{TableName: "audit_logs"},
	}

	event := &Event{
		TenantID:   "bench-tenant",
		UserID:     "bench-user",
		Action:     ActionCreate,
		Resource:   ResourceAgent,
		ResourceID: "agent-bench",
		Result:     ResultSuccess,
	}

	// Expect N queries (one per benchmark iteration)
	for i := 0; i < b.N; i++ {
		mock.ExpectQuery("INSERT INTO audit_logs").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(i + 1)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = al.Log(context.Background(), event)
	}
}
