// Package database provides database utilities and migration management.
package database

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	migratedb "github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// MigrationConfig holds migration configuration.
type MigrationConfig struct {
	// MigrationsFS is the preferred source: an embedded or real filesystem
	// containing migration files. When set, MigrationsPath is ignored.
	MigrationsFS fs.FS

	// MigrationsPath is the directory containing migration files.
	// Used only when MigrationsFS is nil (legacy file:// mode).
	MigrationsPath string

	// DatabaseName is used for the migration lock table.
	DatabaseName string
}

// DefaultMigrationConfig returns default migration configuration.
func DefaultMigrationConfig() MigrationConfig {
	return MigrationConfig{
		MigrationsPath: "migrations",
		DatabaseName:   "aether",
	}
}

// newMigrate constructs a migrate.Migrate from config and an already-created postgres driver.
func newMigrate(config MigrationConfig, driver migratedb.Driver) (*migrate.Migrate, error) {
	if config.MigrationsFS != nil {
		src, err := iofs.New(config.MigrationsFS, ".")
		if err != nil {
			return nil, fmt.Errorf("failed to create iofs source: %w", err)
		}
		m, err := migrate.NewWithInstance("iofs", src, config.DatabaseName, driver)
		if err != nil {
			return nil, fmt.Errorf("failed to create migration instance: %w", err)
		}
		return m, nil
	}

	m, err := migrate.NewWithDatabaseInstance(
		fmt.Sprintf("file://%s", config.MigrationsPath),
		config.DatabaseName,
		driver,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration instance: %w", err)
	}
	return m, nil
}

// RunMigrations runs database migrations up to the latest version.
func RunMigrations(logger *slog.Logger, db *sql.DB, config MigrationConfig) error {
	if config.MigrationsPath == "" && config.MigrationsFS == nil {
		config.MigrationsPath = "migrations"
	}

	if config.DatabaseName == "" {
		config.DatabaseName = "aether"
	}

	if config.MigrationsFS != nil {
		logger.Info("starting database migrations", "source", "embedded")
	} else {
		logger.Info("starting database migrations", "path", config.MigrationsPath)
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{
		DatabaseName: config.DatabaseName,
	})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %w", err)
	}

	m, err := newMigrate(config, driver)
	if err != nil {
		return err
	}
	defer m.Close()

	// Get current version before migration
	currentVersion, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("failed to get current migration version: %w", err)
	}

	if dirty {
		logger.Error("database is in dirty state - manual intervention required",
			"version", currentVersion,
		)
		return fmt.Errorf("database is in dirty state at version %d - please fix manually", currentVersion)
	}

	logger.Info("current database version", "version", currentVersion, "dirty", dirty)

	// Run migrations up
	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Get new version
	newVersion, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("failed to get migration version after migration: %w", err)
	}

	if dirty {
		logger.Error("database is in dirty state after migration",
			"version", newVersion,
		)
		return fmt.Errorf("database is in dirty state at version %d after migration", newVersion)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		logger.Info("database schema is up to date", "version", currentVersion)
	} else {
		logger.Info("database migrations completed successfully",
			"from_version", currentVersion,
			"to_version", newVersion,
		)
	}

	return nil
}

// MigrateDown rolls back the last migration.
// WARNING: This can result in data loss!
func MigrateDown(logger *slog.Logger, db *sql.DB, config MigrationConfig) error {
	if config.MigrationsPath == "" && config.MigrationsFS == nil {
		config.MigrationsPath = "migrations"
	}

	if config.DatabaseName == "" {
		config.DatabaseName = "aether"
	}

	logger.Warn("rolling back last migration - this may result in data loss")

	driver, err := postgres.WithInstance(db, &postgres.Config{
		DatabaseName: config.DatabaseName,
	})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %w", err)
	}

	m, err := newMigrate(config, driver)
	if err != nil {
		return err
	}
	defer m.Close()

	currentVersion, dirty, err := m.Version()
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	if dirty {
		return fmt.Errorf("database is in dirty state at version %d", currentVersion)
	}

	if err := m.Steps(-1); err != nil {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	newVersion, _, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("failed to get version after rollback: %w", err)
	}

	logger.Info("migration rolled back successfully",
		"from_version", currentVersion,
		"to_version", newVersion,
	)

	return nil
}

// GetVersion returns the current migration version.
func GetVersion(logger *slog.Logger, db *sql.DB, config MigrationConfig) (uint, bool, error) {
	if config.DatabaseName == "" {
		config.DatabaseName = "aether"
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{
		DatabaseName: config.DatabaseName,
	})
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migration driver: %w", err)
	}

	m, err := newMigrate(config, driver)
	if err != nil {
		return 0, false, err
	}
	defer m.Close()

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, fmt.Errorf("failed to get version: %w", err)
	}

	return version, dirty, nil
}
