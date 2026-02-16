// Package main provides a migration runner for database schema updates.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/aether-runtime/aether/internal/database"
	_ "github.com/lib/pq"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable not set")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("failed to close database: %v", err)
		}
	}()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	config := database.DefaultMigrationConfig()
	config.MigrationsPath = "migrations"

	fmt.Println("Running migrations...")
	if err := database.RunMigrations(logger, db, config); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	fmt.Println("Migrations completed successfully!")
}
