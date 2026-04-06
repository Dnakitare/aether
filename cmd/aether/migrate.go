package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"

	"github.com/dnakitare/aether/internal/database"
	"github.com/dnakitare/aether/migrations"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Manage database schema migrations",
	Long:  "Run, rollback, or inspect database schema migrations.",
}

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply all pending migrations",
	Long:  "Apply all pending database migrations to bring the schema up to date. Safe to run multiple times.",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, cfg, err := openMigrateConn()
		if err != nil {
			return err
		}
		defer db.Close()

		fmt.Println("Running migrations...")
		if err := database.RunMigrations(logger, db, cfg); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		fmt.Println("Done.")
		return nil
	},
}

var migrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Roll back the last migration",
	Long:  "Roll back the most recently applied migration. This may result in data loss.",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, cfg, err := openMigrateConn()
		if err != nil {
			return err
		}
		defer db.Close()

		fmt.Println("Rolling back last migration...")
		if err := database.MigrateDown(logger, db, cfg); err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}
		fmt.Println("Done.")
		return nil
	},
}

var migrateVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show current migration version",
	Long:  "Display the current database schema migration version.",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, cfg, err := openMigrateConn()
		if err != nil {
			return err
		}
		defer db.Close()

		version, dirty, err := database.GetVersion(logger, db, cfg)
		if err != nil {
			return fmt.Errorf("failed to get version: %w", err)
		}

		switch {
		case dirty:
			fmt.Printf("version: %d (DIRTY — manual intervention required)\n", version)
		case version == 0:
			fmt.Println("version: none (no migrations applied)")
		default:
			fmt.Printf("version: %d\n", version)
		}
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)
	migrateCmd.AddCommand(migrateVersionCmd)
	rootCmd.AddCommand(migrateCmd)
}

// openMigrateConn opens a PostgreSQL connection using DATABASE_URL and returns
// the db handle and migration config. Falls back to a local dev DSN when
// DATABASE_URL is not set. The caller is responsible for closing the db.
func openMigrateConn() (*sql.DB, database.MigrationConfig, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// #nosec G101 — default dev credentials; override via DATABASE_URL in production
		dsn = "postgres://postgres:postgres@localhost:5432/aether?sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, database.MigrationConfig{}, fmt.Errorf("failed to open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, database.MigrationConfig{}, fmt.Errorf("failed to connect to database: %w", err)
	}

	cfg := database.MigrationConfig{
		MigrationsFS: migrations.FS,
		DatabaseName: "aether",
	}
	return db, cfg, nil
}
