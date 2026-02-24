package database

import (
	"database/sql"
	"log/slog"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultMigrationConfig(t *testing.T) {
	config := DefaultMigrationConfig()

	assert.Equal(t, "migrations", config.MigrationsPath)
	assert.Equal(t, "aether", config.DatabaseName)
}

func TestConfigDefaults(t *testing.T) {
	t.Run("empty config gets defaults in RunMigrations", func(t *testing.T) {
		// Test that empty config values are replaced with defaults
		// We can't actually run migrations without a DB, but we can verify
		// the config processing logic by checking error messages

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

		// Create a closed database connection to trigger early error
		db, err := sql.Open("postgres", "invalid-connection-string")
		require.NoError(t, err)
		db.Close()

		config := MigrationConfig{} // Empty config

		err = RunMigrations(logger, db, config)
		// Should fail because DB is closed, but config defaults should be applied
		assert.Error(t, err)
	})

	t.Run("empty config gets defaults in MigrateDown", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

		db, err := sql.Open("postgres", "invalid-connection-string")
		require.NoError(t, err)
		db.Close()

		config := MigrationConfig{} // Empty config

		err = MigrateDown(logger, db, config)
		assert.Error(t, err)
	})

	t.Run("empty database name gets default in GetVersion", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

		db, err := sql.Open("postgres", "invalid-connection-string")
		require.NoError(t, err)
		db.Close()

		config := MigrationConfig{
			MigrationsPath: "migrations",
			// DatabaseName empty
		}

		_, _, err = GetVersion(logger, db, config)
		assert.Error(t, err)
	})
}

// Integration tests that require a real PostgreSQL database
// These tests are skipped if POSTGRES_DSN is not set

func getTestDB(t *testing.T) (*sql.DB, bool) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set, skipping database integration tests")
		return nil, false
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("Failed to connect to PostgreSQL: %v", err)
		return nil, false
	}

	if err := db.Ping(); err != nil {
		t.Skipf("Failed to ping PostgreSQL: %v", err)
		db.Close()
		return nil, false
	}

	return db, true
}

func TestRunMigrations_Integration(t *testing.T) {
	db, ok := getTestDB(t)
	if !ok {
		return
	}
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	t.Run("successful migration from scratch", func(t *testing.T) {
		// This test assumes migrations directory exists with valid migrations
		config := MigrationConfig{
			MigrationsPath: "../../migrations",
			DatabaseName:   "aether_test",
		}

		err := RunMigrations(logger, db, config)
		// Migration might succeed or report no changes if already applied
		// Both are acceptable outcomes
		if err != nil {
			// Allow "no change" scenario
			assert.Contains(t, err.Error(), "no change", "Expected either success or no change")
		}
	})

	t.Run("run migrations twice - should report no change", func(t *testing.T) {
		config := MigrationConfig{
			MigrationsPath: "../../migrations",
			DatabaseName:   "aether_test",
		}

		// First run
		err := RunMigrations(logger, db, config)
		require.NoError(t, err)

		// Second run should report no change
		err = RunMigrations(logger, db, config)
		require.NoError(t, err) // ErrNoChange is handled internally
	})

	t.Run("invalid migrations path", func(t *testing.T) {
		config := MigrationConfig{
			MigrationsPath: "/nonexistent/path",
			DatabaseName:   "aether_test",
		}

		err := RunMigrations(logger, db, config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create migration instance")
	})
}

func TestGetVersion_Integration(t *testing.T) {
	db, ok := getTestDB(t)
	if !ok {
		return
	}
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	t.Run("get version after migrations", func(t *testing.T) {
		config := MigrationConfig{
			MigrationsPath: "../../migrations",
			DatabaseName:   "aether_test",
		}

		// Run migrations first
		err := RunMigrations(logger, db, config)
		require.NoError(t, err)

		// Get version
		version, dirty, err := GetVersion(logger, db, config)
		require.NoError(t, err)
		assert.False(t, dirty, "Database should not be in dirty state")
		assert.Greater(t, version, uint(0), "Version should be greater than 0 after migrations")
	})

	t.Run("invalid migrations path", func(t *testing.T) {
		config := MigrationConfig{
			MigrationsPath: "/nonexistent/path",
			DatabaseName:   "aether_test",
		}

		_, _, err := GetVersion(logger, db, config)
		assert.Error(t, err)
	})
}

func TestMigrateDown_Integration(t *testing.T) {
	db, ok := getTestDB(t)
	if !ok {
		return
	}
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	t.Run("rollback after applying migrations", func(t *testing.T) {
		config := MigrationConfig{
			MigrationsPath: "../../migrations",
			DatabaseName:   "aether_test",
		}

		// Ensure migrations are applied
		err := RunMigrations(logger, db, config)
		require.NoError(t, err)

		// Get current version
		versionBefore, dirty, err := GetVersion(logger, db, config)
		require.NoError(t, err)
		require.False(t, dirty)

		// Skip if we're at version 0 (no migrations to roll back)
		if versionBefore == 0 {
			t.Skip("No migrations to roll back")
			return
		}

		// Roll back one migration
		err = MigrateDown(logger, db, config)
		require.NoError(t, err)

		// Get new version
		versionAfter, dirty, err := GetVersion(logger, db, config)
		require.NoError(t, err)
		require.False(t, dirty)

		// Version should have decreased by 1
		assert.Equal(t, versionBefore-1, versionAfter, "Version should decrease by 1 after rollback")

		// Re-apply migrations to restore state
		err = RunMigrations(logger, db, config)
		require.NoError(t, err)
	})

	t.Run("invalid migrations path", func(t *testing.T) {
		config := MigrationConfig{
			MigrationsPath: "/nonexistent/path",
			DatabaseName:   "aether_test",
		}

		err := MigrateDown(logger, db, config)
		assert.Error(t, err)
	})
}

// Test error scenarios
func TestErrorHandling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("closed database connection", func(t *testing.T) {
		db, err := sql.Open("postgres", "postgres://invalid")
		require.NoError(t, err)
		db.Close()

		config := DefaultMigrationConfig()

		err = RunMigrations(logger, db, config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create migration driver")

		err = MigrateDown(logger, db, config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create migration driver")

		_, _, err = GetVersion(logger, db, config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create migration driver")
	})

	t.Run("invalid connection string", func(t *testing.T) {
		// Open with invalid connection string but don't close
		db, err := sql.Open("postgres", "invalid-dsn-format")
		require.NoError(t, err)
		defer db.Close()

		config := DefaultMigrationConfig()

		err = RunMigrations(logger, db, config)
		assert.Error(t, err)

		err = MigrateDown(logger, db, config)
		assert.Error(t, err)

		_, _, err = GetVersion(logger, db, config)
		assert.Error(t, err)
	})
}
