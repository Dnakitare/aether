package backup_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/backup"
)

func TestBackupConfig(t *testing.T) {
	config := backup.DefaultBackupConfig()

	if config.BackupDir == "" {
		t.Error("Expected BackupDir to be set")
	}

	if config.Schedule == "" {
		t.Error("Expected Schedule to be set")
	}

	if config.RetentionDays == 0 {
		t.Error("Expected RetentionDays to be set")
	}
}

func TestRestoreConfig(t *testing.T) {
	config := backup.DefaultRestoreConfig()

	if config.BackupDir == "" {
		t.Error("Expected BackupDir to be set")
	}

	if !config.VerifyBeforeRestore {
		t.Error("Expected VerifyBeforeRestore to be true by default")
	}
}

func TestDRConfig(t *testing.T) {
	config := backup.DefaultDRConfig()

	if config.RecoveryTimeObjective == 0 {
		t.Error("Expected RecoveryTimeObjective to be set")
	}

	if config.RecoveryPointObjective == 0 {
		t.Error("Expected RecoveryPointObjective to be set")
	}

	if config.PrimaryRegion == "" {
		t.Error("Expected PrimaryRegion to be set")
	}

	if config.SecondaryRegion == "" {
		t.Error("Expected SecondaryRegion to be set")
	}
}

func TestBackupManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Create temporary backup directory
	tmpDir := t.TempDir()

	config := backup.DefaultBackupConfig()
	config.BackupDir = tmpDir

	manager := backup.NewBackupManager(logger, config, nil, nil)

	// Test ListBackups on empty directory
	backups, err := manager.ListBackups()
	if err != nil {
		t.Fatalf("Failed to list backups: %v", err)
	}

	if len(backups) != 0 {
		t.Errorf("Expected 0 backups, got %d", len(backups))
	}
}

func TestBackupMetadata(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()

	config := backup.DefaultBackupConfig()
	config.BackupDir = tmpDir

	manager := backup.NewBackupManager(logger, config, nil, nil)

	// Create a test backup directory with metadata
	backupID := "test-backup"
	backupPath := filepath.Join(tmpDir, backupID)
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		t.Fatalf("Failed to create backup directory: %v", err)
	}

	// Write test metadata
	metadataPath := filepath.Join(backupPath, "metadata.json")
	metadataContent := `{
		"id": "test-backup",
		"timestamp": "2024-01-01T00:00:00Z",
		"size": 1024,
		"components": ["postgres", "redis"],
		"compression": true,
		"encryption": false,
		"postgres_size": 512,
		"redis_size": 512,
		"state_size": 0
	}`

	if err := os.WriteFile(metadataPath, []byte(metadataContent), 0644); err != nil {
		t.Fatalf("Failed to write metadata: %v", err)
	}

	// Test GetBackup
	metadata, err := manager.GetBackup(backupID)
	if err != nil {
		t.Fatalf("Failed to get backup: %v", err)
	}

	if metadata.ID != backupID {
		t.Errorf("Expected ID = %s, got %s", backupID, metadata.ID)
	}

	if len(metadata.Components) != 2 {
		t.Errorf("Expected 2 components, got %d", len(metadata.Components))
	}
}

func TestBackupCleanup(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()

	config := backup.DefaultBackupConfig()
	config.BackupDir = tmpDir
	config.RetentionDays = 7

	manager := backup.NewBackupManager(logger, config, nil, nil)

	ctx := context.Background()

	// Create test backups with different ages
	oldBackupID := "old-backup"
	oldBackupPath := filepath.Join(tmpDir, oldBackupID)
	if err := os.MkdirAll(oldBackupPath, 0755); err != nil {
		t.Fatalf("Failed to create old backup directory: %v", err)
	}

	// Create metadata for old backup (30 days old)
	oldTime := time.Now().AddDate(0, 0, -30)
	oldMetadataPath := filepath.Join(oldBackupPath, "metadata.json")
	oldMetadataContent := `{
		"id": "old-backup",
		"timestamp": "` + oldTime.Format(time.RFC3339) + `",
		"size": 1024,
		"components": ["postgres"],
		"compression": true,
		"encryption": false,
		"postgres_size": 1024,
		"redis_size": 0,
		"state_size": 0
	}`

	if err := os.WriteFile(oldMetadataPath, []byte(oldMetadataContent), 0644); err != nil {
		t.Fatalf("Failed to write old metadata: %v", err)
	}

	// Test cleanup
	if err := manager.CleanupOldBackups(ctx); err != nil {
		t.Fatalf("Failed to cleanup old backups: %v", err)
	}

	// Verify old backup was deleted
	if _, err := os.Stat(oldBackupPath); !os.IsNotExist(err) {
		t.Error("Expected old backup to be deleted")
	}
}

func TestRestoreManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()

	config := backup.DefaultRestoreConfig()
	config.BackupDir = tmpDir

	manager := backup.NewRestoreManager(logger, config, nil, nil)

	// Test ListRestorePoints on empty directory
	restorePoints, err := manager.ListRestorePoints()
	if err != nil {
		t.Fatalf("Failed to list restore points: %v", err)
	}

	if len(restorePoints) != 0 {
		t.Errorf("Expected 0 restore points, got %d", len(restorePoints))
	}
}

func TestPointInTimeRestore(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()

	config := backup.DefaultRestoreConfig()
	config.BackupDir = tmpDir
	config.PointInTime = true

	manager := backup.NewRestoreManager(logger, config, nil, nil)

	ctx := context.Background()

	// Test with no backups
	targetTime := time.Now().Add(-1 * time.Hour)
	_, err := manager.RestorePointInTime(ctx, targetTime)
	if err == nil {
		t.Error("Expected error when no backups available")
	}
}

func TestDisasterRecoveryManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()

	backupConfig := backup.DefaultBackupConfig()
	backupConfig.BackupDir = tmpDir

	restoreConfig := backup.DefaultRestoreConfig()
	restoreConfig.BackupDir = tmpDir

	drConfig := backup.DefaultDRConfig()

	backupMgr := backup.NewBackupManager(logger, backupConfig, nil, nil)
	restoreMgr := backup.NewRestoreManager(logger, restoreConfig, nil, nil)

	drManager := backup.NewDisasterRecoveryManager(logger, backupMgr, restoreMgr, drConfig)

	ctx := context.Background()

	// Test GetDRStatus
	status, err := drManager.GetDRStatus(ctx)
	if err != nil {
		t.Fatalf("Failed to get DR status: %v", err)
	}

	if status.CurrentRegion != drConfig.PrimaryRegion {
		t.Errorf("Expected current region = %s, got %s", drConfig.PrimaryRegion, status.CurrentRegion)
	}
}

func TestDRStatusRPO(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()

	backupConfig := backup.DefaultBackupConfig()
	backupConfig.BackupDir = tmpDir

	restoreConfig := backup.DefaultRestoreConfig()
	restoreConfig.BackupDir = tmpDir

	drConfig := backup.DefaultDRConfig()
	drConfig.RecoveryPointObjective = 1 * time.Hour

	backupMgr := backup.NewBackupManager(logger, backupConfig, nil, nil)
	restoreMgr := backup.NewRestoreManager(logger, restoreConfig, nil, nil)

	drManager := backup.NewDisasterRecoveryManager(logger, backupMgr, restoreMgr, drConfig)

	// Create test backup
	backupID := "test-backup"
	backupPath := filepath.Join(tmpDir, backupID)
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		t.Fatalf("Failed to create backup directory: %v", err)
	}

	// Recent backup (within RPO)
	recentTime := time.Now().Add(-30 * time.Minute)
	metadataPath := filepath.Join(backupPath, "metadata.json")
	metadataContent := `{
		"id": "test-backup",
		"timestamp": "` + recentTime.Format(time.RFC3339) + `",
		"size": 1024,
		"components": ["postgres"],
		"compression": true,
		"encryption": false,
		"postgres_size": 1024,
		"redis_size": 0,
		"state_size": 0
	}`

	if err := os.WriteFile(metadataPath, []byte(metadataContent), 0644); err != nil {
		t.Fatalf("Failed to write metadata: %v", err)
	}

	ctx := context.Background()

	status, err := drManager.GetDRStatus(ctx)
	if err != nil {
		t.Fatalf("Failed to get DR status: %v", err)
	}

	if !status.MeetsRPO {
		t.Error("Expected to meet RPO with recent backup")
	}
}

func TestBackupVerification(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()

	config := backup.DefaultBackupConfig()
	config.BackupDir = tmpDir

	manager := backup.NewBackupManager(logger, config, nil, nil)

	// Test verify on non-existent backup
	err := manager.VerifyBackup("non-existent")
	if err == nil {
		t.Error("Expected error when verifying non-existent backup")
	}
}
