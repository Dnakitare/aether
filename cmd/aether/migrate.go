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
	Short: "Run database migrations and exit",
	Long:  "Applies all pending database migrations. Safe to run multiple times (idempotent).",
	RunE:  runMigrate,
}

func init() {
	rootCmd.AddCommand(migrateCmd)
}

func runMigrate(cmd *cobra.Command, args []string) error {
	postgresURL := os.Getenv("DATABASE_URL")
	if postgresURL == "" {
		// #nosec G101 - Default development credentials, overridden by DATABASE_URL env var in production
		postgresURL = "postgres://postgres:postgres@localhost:5432/aether?sslmode=disable"
	}

	db, err := sql.Open("postgres", postgresURL)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	migConfig := database.MigrationConfig{
		MigrationsFS: migrations.FS,
		DatabaseName: "aether",
	}

	return database.RunMigrations(logger, db, migConfig)
}
