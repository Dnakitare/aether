// Package auth provides authentication and authorization.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// APIKeyManager manages API keys for service accounts.
type APIKeyManager struct {
	logger *slog.Logger
	mu     sync.RWMutex

	// API keys by hashed key
	keys map[string]*APIKey

	// Keys by tenant for listing
	tenantKeys map[api.TenantID][]string
}

// APIKey represents an API key.
type APIKey struct {
	ID         string
	Name       string
	TenantID   api.TenantID
	KeyHash    string // SHA-256 hash of the actual key
	Prefix     string // First 8 chars for identification
	Scopes     []Permission
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	Revoked    bool
}

// NewAPIKeyManager creates a new API key manager.
func NewAPIKeyManager(logger *slog.Logger) *APIKeyManager {
	return &APIKeyManager{
		logger:     logger.With("component", "apikey_manager"),
		keys:       make(map[string]*APIKey),
		tenantKeys: make(map[api.TenantID][]string),
	}
}

// CreateKey creates a new API key.
func (km *APIKeyManager) CreateKey(ctx context.Context, tenantID api.TenantID, name string, scopes []Permission, ttl time.Duration) (string, *APIKey, error) {
	km.mu.Lock()
	defer km.mu.Unlock()

	// Generate random key
	rawKey, err := generateRandomKey(32)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// Format: aether_<prefix>_<random>
	prefix := rawKey[:8]
	fullKey := fmt.Sprintf("aether_%s_%s", prefix, rawKey[8:])

	// Hash the key for storage
	keyHash := hashKey(fullKey)

	now := time.Now()
	var expiresAt *time.Time
	if ttl > 0 {
		expiry := now.Add(ttl)
		expiresAt = &expiry
	}

	apiKey := &APIKey{
		ID:        generateID("key"),
		Name:      name,
		TenantID:  tenantID,
		KeyHash:   keyHash,
		Prefix:    prefix,
		Scopes:    scopes,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		Revoked:   false,
	}

	km.keys[keyHash] = apiKey
	km.tenantKeys[tenantID] = append(km.tenantKeys[tenantID], keyHash)

	km.logger.InfoContext(ctx,
		"API key created",
		"id", apiKey.ID,
		"tenant_id", tenantID,
		"name", name,
		"prefix", prefix,
	)

	return fullKey, apiKey, nil
}

// ValidateKey validates an API key and returns the associated key info.
func (km *APIKeyManager) ValidateKey(ctx context.Context, key string) (*APIKey, error) {
	keyHash := hashKey(key)

	km.mu.Lock()
	defer km.mu.Unlock()

	apiKey, exists := km.keys[keyHash]
	if !exists {
		return nil, fmt.Errorf("invalid API key")
	}

	if apiKey.Revoked {
		return nil, fmt.Errorf("API key has been revoked")
	}

	if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
		return nil, fmt.Errorf("API key has expired")
	}

	// Update last used timestamp
	now := time.Now()
	apiKey.LastUsedAt = &now

	return apiKey, nil
}

// RevokeKey revokes an API key.
func (km *APIKeyManager) RevokeKey(ctx context.Context, tenantID api.TenantID, keyID string) error {
	km.mu.Lock()
	defer km.mu.Unlock()

	for _, apiKey := range km.keys {
		if apiKey.ID == keyID && apiKey.TenantID == tenantID {
			apiKey.Revoked = true
			km.logger.InfoContext(ctx, "API key revoked", "id", keyID, "tenant_id", tenantID)
			return nil
		}
	}

	return fmt.Errorf("API key not found")
}

// ListKeys lists all API keys for a tenant.
func (km *APIKeyManager) ListKeys(tenantID api.TenantID) []*APIKey {
	km.mu.RLock()
	defer km.mu.RUnlock()

	keyHashes, exists := km.tenantKeys[tenantID]
	if !exists {
		return []*APIKey{}
	}

	keys := make([]*APIKey, 0, len(keyHashes))
	for _, hash := range keyHashes {
		if apiKey, exists := km.keys[hash]; exists && !apiKey.Revoked {
			keys = append(keys, apiKey)
		}
	}

	return keys
}

// RotateKey rotates an API key (creates new, revokes old).
func (km *APIKeyManager) RotateKey(ctx context.Context, tenantID api.TenantID, oldKeyID string, ttl time.Duration) (string, *APIKey, error) {
	// Get old key info
	km.mu.RLock()
	var oldKey *APIKey
	for _, apiKey := range km.keys {
		if apiKey.ID == oldKeyID && apiKey.TenantID == tenantID {
			oldKey = apiKey
			break
		}
	}
	km.mu.RUnlock()

	if oldKey == nil {
		return "", nil, fmt.Errorf("old key not found")
	}

	// Create new key with same scopes
	newKeyStr, newKey, err := km.CreateKey(ctx, tenantID, oldKey.Name+" (rotated)", oldKey.Scopes, ttl)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create new key: %w", err)
	}

	// Revoke old key
	if err := km.RevokeKey(ctx, tenantID, oldKeyID); err != nil {
		km.logger.WarnContext(ctx, "failed to revoke old key", "error", err)
	}

	km.logger.InfoContext(ctx, "API key rotated", "old_id", oldKeyID, "new_id", newKey.ID)

	return newKeyStr, newKey, nil
}

// CleanupExpired removes expired keys.
func (km *APIKeyManager) CleanupExpired(ctx context.Context) int {
	km.mu.Lock()
	defer km.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for hash, apiKey := range km.keys {
		if apiKey.ExpiresAt != nil && now.After(*apiKey.ExpiresAt) {
			delete(km.keys, hash)
			cleaned++
		}
	}

	if cleaned > 0 {
		km.logger.InfoContext(ctx, "cleaned up expired API keys", "count", cleaned)
	}

	return cleaned
}

// generateRandomKey generates a random key of specified length.
func generateRandomKey(length int) (string, error) {
	// Use alphanumeric characters only (no - or _ from base64url)
	// to avoid conflicts with separator character in key format
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Map random bytes to charset
	result := make([]byte, length)
	for i, b := range bytes {
		result[i] = charset[int(b)%len(charset)]
	}

	return string(result), nil
}

// hashKey hashes an API key using SHA-256.
func hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// generateID generates a unique ID with a prefix.
func generateID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
