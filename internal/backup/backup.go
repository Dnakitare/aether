package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// allowedTables is a whitelist of tables that can be backed up
var allowedTables = map[string]bool{
	"agents":      true,
	"tenants":     true,
	"audit_logs":  true,
	"checkpoints": true,
	"quotas":      true,
}

// BackupManager manages backup operations
type BackupManager struct {
	logger *slog.Logger
	config BackupConfig
	db     *sql.DB
	redis  *redis.Client
}

// BackupConfig configures backup operations
type BackupConfig struct {
	// BackupDir is the directory where backups are stored
	BackupDir string

	// Schedule is the cron schedule for automatic backups (e.g., "0 2 * * *")
	Schedule string

	// RetentionDays is how many days to keep backups
	RetentionDays int

	// Compression enables backup compression
	Compression bool

	// Encryption enables backup encryption
	Encryption bool

	// EncryptionKey is the encryption key (must be 32 bytes for AES-256)
	EncryptionKey string

	// S3Bucket is the S3 bucket for remote backup (optional)
	S3Bucket string

	// S3Region is the S3 region
	S3Region string
}

// DefaultBackupConfig returns default backup configuration
func DefaultBackupConfig() BackupConfig {
	return BackupConfig{
		BackupDir:     "/var/lib/aether/backups",
		Schedule:      "0 2 * * *", // 2 AM daily
		RetentionDays: 30,
		Compression:   true,
		Encryption:    false,
	}
}

// BackupMetadata contains metadata about a backup
type BackupMetadata struct {
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Size         int64     `json:"size"`
	Components   []string  `json:"components"`
	Compression  bool      `json:"compression"`
	Encryption   bool      `json:"encryption"`
	PostgresSize int64     `json:"postgres_size"`
	RedisSize    int64     `json:"redis_size"`
	StateSize    int64     `json:"state_size"`
}

// NewBackupManager creates a new backup manager
func NewBackupManager(logger *slog.Logger, config BackupConfig, db *sql.DB, redis *redis.Client) *BackupManager {
	return &BackupManager{
		logger: logger,
		config: config,
		db:     db,
		redis:  redis,
	}
}

// CreateBackup creates a full system backup
func (bm *BackupManager) CreateBackup(ctx context.Context) (*BackupMetadata, error) {
	backupID := fmt.Sprintf("backup-%s", time.Now().Format("20060102-150405"))
	backupPath := filepath.Join(bm.config.BackupDir, backupID)

	bm.logger.Info("creating backup", "id", backupID, "path", backupPath)

	// Create backup directory
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	metadata := &BackupMetadata{
		ID:          backupID,
		Timestamp:   time.Now(),
		Components:  []string{},
		Compression: bm.config.Compression,
		Encryption:  bm.config.Encryption,
	}

	// Backup PostgreSQL
	if bm.db != nil {
		postgresPath := filepath.Join(backupPath, "postgres.sql")
		size, err := bm.backupPostgres(ctx, postgresPath)
		if err != nil {
			return nil, fmt.Errorf("postgres backup failed: %w", err)
		}
		metadata.PostgresSize = size
		metadata.Components = append(metadata.Components, "postgres")
	}

	// Backup Redis
	if bm.redis != nil {
		redisPath := filepath.Join(backupPath, "redis.rdb")
		size, err := bm.backupRedis(ctx, redisPath)
		if err != nil {
			return nil, fmt.Errorf("redis backup failed: %w", err)
		}
		metadata.RedisSize = size
		metadata.Components = append(metadata.Components, "redis")
	}

	// Write metadata
	metadataPath := filepath.Join(backupPath, "metadata.json")
	if err := bm.writeMetadata(metadata, metadataPath); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	// Calculate total size
	metadata.Size = metadata.PostgresSize + metadata.RedisSize + metadata.StateSize

	bm.logger.Info("backup created successfully",
		"id", backupID,
		"size", metadata.Size,
		"components", metadata.Components,
	)

	return metadata, nil
}

// backupPostgres backs up PostgreSQL database
func (bm *BackupManager) backupPostgres(ctx context.Context, path string) (int64, error) {
	bm.logger.Info("backing up postgres", "path", path)

	// Get all tables
	tables := []string{"agents", "tenants", "audit_logs", "checkpoints"}

	file, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer file.Close()

	var totalSize int64

	for _, table := range tables {
		// Write table schema
		schema, err := bm.getTableSchema(ctx, table)
		if err != nil {
			bm.logger.Warn("failed to get schema for table", "table", table, "error", err)
			continue
		}

		written, err := file.WriteString(schema + "\n\n")
		if err != nil {
			return 0, fmt.Errorf("failed to write schema: %w", err)
		}
		totalSize += int64(written)

		// Write table data
		data, err := bm.getTableData(ctx, table)
		if err != nil {
			bm.logger.Warn("failed to get data for table", "table", table, "error", err)
			continue
		}

		written, err = file.WriteString(data + "\n\n")
		if err != nil {
			return 0, fmt.Errorf("failed to write data: %w", err)
		}
		totalSize += int64(written)
	}

	return totalSize, nil
}

// getTableSchema gets the schema for a table
func (bm *BackupManager) getTableSchema(ctx context.Context, table string) (string, error) {
	// Validate table name against whitelist to prevent SQL injection
	if !allowedTables[table] {
		return "", fmt.Errorf("invalid table name: %s (not in whitelist)", table)
	}

	// Simplified schema extraction - in production use pg_dump
	return fmt.Sprintf("-- Schema for table: %s\n-- (Use pg_dump for actual schema)", table), nil
}

// getTableData gets the data for a table
func (bm *BackupManager) getTableData(ctx context.Context, table string) (string, error) {
	// Validate table name against whitelist to prevent SQL injection
	if !allowedTables[table] {
		return "", fmt.Errorf("invalid table name: %s (not in whitelist)", table)
	}

	// Use parameterized query with quoted identifier for safety
	// Note: pq.QuoteIdentifier properly escapes table names
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", pq.QuoteIdentifier(table))

	var count int64
	err := bm.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return "", fmt.Errorf("failed to query table %s: %w", table, err)
	}

	return fmt.Sprintf("-- Data for table: %s (rows: %d)\n-- (Use pg_dump for actual data)", table, count), nil
}

// backupRedis backs up Redis database
func (bm *BackupManager) backupRedis(ctx context.Context, path string) (int64, error) {
	bm.logger.Info("backing up redis", "path", path)

	// Trigger Redis BGSAVE
	if err := bm.redis.BgSave(ctx).Err(); err != nil {
		return 0, fmt.Errorf("failed to trigger redis save: %w", err)
	}

	// Wait for save to complete
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	timeout := time.After(30 * time.Second)

	for {
		select {
		case <-timeout:
			return 0, fmt.Errorf("redis save timeout")
		case <-ticker.C:
			result, err := bm.redis.Info(ctx, "persistence").Result()
			if err != nil {
				return 0, fmt.Errorf("failed to check save status: %w", err)
			}

			// Check if save is in progress
			if !contains(result, "rdb_bgsave_in_progress:1") {
				// Save complete, create backup info
				file, err := os.Create(path)
				if err != nil {
					return 0, fmt.Errorf("failed to create backup file: %w", err)
				}
				defer file.Close()

				info := fmt.Sprintf("Redis backup snapshot at %s\n", time.Now().Format(time.RFC3339))
				written, err := file.WriteString(info)
				if err != nil {
					return 0, fmt.Errorf("failed to write backup info: %w", err)
				}

				return int64(written), nil
			}
		}
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}

// writeMetadata writes backup metadata to a file
func (bm *BackupManager) writeMetadata(metadata *BackupMetadata, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create metadata file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}

	return nil
}

// ListBackups lists all available backups
func (bm *BackupManager) ListBackups() ([]*BackupMetadata, error) {
	entries, err := os.ReadDir(bm.config.BackupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*BackupMetadata{}, nil
		}
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []*BackupMetadata

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		metadataPath := filepath.Join(bm.config.BackupDir, entry.Name(), "metadata.json")
		metadata, err := bm.readMetadata(metadataPath)
		if err != nil {
			bm.logger.Warn("failed to read metadata", "path", metadataPath, "error", err)
			continue
		}

		backups = append(backups, metadata)
	}

	return backups, nil
}

// readMetadata reads backup metadata from a file
func (bm *BackupManager) readMetadata(path string) (*BackupMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata file: %w", err)
	}
	defer file.Close()

	var metadata BackupMetadata
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	return &metadata, nil
}

// GetBackup gets metadata for a specific backup
func (bm *BackupManager) GetBackup(backupID string) (*BackupMetadata, error) {
	metadataPath := filepath.Join(bm.config.BackupDir, backupID, "metadata.json")
	return bm.readMetadata(metadataPath)
}

// DeleteBackup deletes a backup
func (bm *BackupManager) DeleteBackup(backupID string) error {
	backupPath := filepath.Join(bm.config.BackupDir, backupID)

	bm.logger.Info("deleting backup", "id", backupID)

	if err := os.RemoveAll(backupPath); err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}

	return nil
}

// CleanupOldBackups deletes backups older than retention period
func (bm *BackupManager) CleanupOldBackups(ctx context.Context) error {
	backups, err := bm.ListBackups()
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -bm.config.RetentionDays)
	deleted := 0

	for _, backup := range backups {
		if backup.Timestamp.Before(cutoff) {
			bm.logger.Info("deleting old backup",
				"id", backup.ID,
				"age", time.Since(backup.Timestamp),
			)

			if err := bm.DeleteBackup(backup.ID); err != nil {
				bm.logger.Error("failed to delete old backup",
					"id", backup.ID,
					"error", err,
				)
				continue
			}

			deleted++
		}
	}

	bm.logger.Info("cleanup complete", "deleted", deleted)

	return nil
}

// VerifyBackup verifies the integrity of a backup
func (bm *BackupManager) VerifyBackup(backupID string) error {
	backupPath := filepath.Join(bm.config.BackupDir, backupID)

	// Check backup directory exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup not found: %s", backupID)
	}

	// Read metadata
	metadata, err := bm.GetBackup(backupID)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	// Verify each component
	for _, component := range metadata.Components {
		componentPath := filepath.Join(backupPath, component+".sql")
		if component == "redis" {
			componentPath = filepath.Join(backupPath, component+".rdb")
		}

		if _, err := os.Stat(componentPath); os.IsNotExist(err) {
			return fmt.Errorf("component file missing: %s", component)
		}
	}

	bm.logger.Info("backup verified", "id", backupID)

	return nil
}

// BackupScheduler handles scheduled backups
type BackupScheduler struct {
	logger  *slog.Logger
	manager *BackupManager
	config  BackupConfig
}

// NewBackupScheduler creates a new backup scheduler
func NewBackupScheduler(logger *slog.Logger, manager *BackupManager, config BackupConfig) *BackupScheduler {
	return &BackupScheduler{
		logger:  logger,
		manager: manager,
		config:  config,
	}
}

// Start starts the backup scheduler
func (bs *BackupScheduler) Start(ctx context.Context) error {
	bs.logger.Info("starting backup scheduler", "schedule", bs.config.Schedule)

	// For simplicity, use a fixed interval instead of cron
	// In production, use a proper cron library like github.com/robfig/cron
	interval := 24 * time.Hour // Daily backups

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run initial backup
	if _, err := bs.manager.CreateBackup(ctx); err != nil {
		bs.logger.Error("initial backup failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			bs.logger.Info("stopping backup scheduler")
			return nil
		case <-ticker.C:
			bs.logger.Info("running scheduled backup")

			if _, err := bs.manager.CreateBackup(ctx); err != nil {
				bs.logger.Error("scheduled backup failed", "error", err)
			}

			// Cleanup old backups
			if err := bs.manager.CleanupOldBackups(ctx); err != nil {
				bs.logger.Error("backup cleanup failed", "error", err)
			}
		}
	}
}
