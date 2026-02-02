// Package api provides the HTTP API server.
package api

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/aether-runtime/aether/pkg/api"
)

// Validation regex patterns
var (
	// agentNameRegex validates agent names: alphanumeric, hyphens, underscores, max 64 chars
	agentNameRegex = regexp.MustCompile(`^[a-zA-Z0-9-_]{1,64}$`)

	// tenantIDRegex validates tenant IDs: alphanumeric, hyphens, max 64 chars
	tenantIDRegex = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)

	// agentIDRegex validates agent IDs: alphanumeric, hyphens, max 64 chars
	agentIDRegex = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)
)

// Allowed container registries (whitelist)
var allowedRegistries = []string{
	"docker.io/library/",
	"docker.io/",
	"gcr.io/aether/",
	"registry.aether.internal/",
	"ghcr.io/",
}

// Resource limits
const (
	minCPUCount = 1
	maxCPUCount = 64

	minMemoryMB = 128    // 128 MB
	maxMemoryMB = 131072 // 128 GB

	minDiskMB = 1024    // 1 GB
	maxDiskMB = 1048576 // 1 TB
)

// ValidateAgentName validates an agent name.
func ValidateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name is required")
	}
	if len(name) > 64 {
		return fmt.Errorf("agent name too long (max 64 characters)")
	}
	if !agentNameRegex.MatchString(name) {
		return fmt.Errorf("agent name must contain only alphanumeric characters, hyphens, and underscores")
	}
	return nil
}

// ValidateTenantID validates a tenant ID.
func ValidateTenantID(tenantID api.TenantID) error {
	if tenantID == "" {
		return fmt.Errorf("tenant ID is required")
	}
	if len(string(tenantID)) > 64 {
		return fmt.Errorf("tenant ID too long (max 64 characters)")
	}
	if !tenantIDRegex.MatchString(string(tenantID)) {
		return fmt.Errorf("tenant ID must contain only alphanumeric characters and hyphens")
	}
	return nil
}

// ValidateAgentID validates an agent ID.
func ValidateAgentID(agentID api.AgentID) error {
	if agentID == "" {
		return fmt.Errorf("agent ID is required")
	}
	if len(string(agentID)) > 64 {
		return fmt.Errorf("agent ID too long (max 64 characters)")
	}
	if !agentIDRegex.MatchString(string(agentID)) {
		return fmt.Errorf("agent ID must contain only alphanumeric characters and hyphens")
	}
	return nil
}

// ValidateImage validates a container image reference.
func ValidateImage(image string) error {
	if image == "" {
		return fmt.Errorf("image is required")
	}

	// Check against registry whitelist
	allowed := false
	for _, prefix := range allowedRegistries {
		if strings.HasPrefix(image, prefix) {
			allowed = true
			break
		}
	}

	if !allowed {
		return fmt.Errorf("image must be from an allowed registry: %v", allowedRegistries)
	}

	// Additional validation: check for tag or digest
	if !strings.Contains(image, ":") && !strings.Contains(image, "@") {
		return fmt.Errorf("image must include a tag (e.g., :latest) or digest")
	}

	return nil
}

// ValidateResources validates resource requests.
func ValidateResources(resources api.Resources) error {
	// Validate CPU count
	if resources.CPUCount < minCPUCount {
		return fmt.Errorf("CPU count must be at least %d", minCPUCount)
	}
	if resources.CPUCount > maxCPUCount {
		return fmt.Errorf("CPU count must be at most %d", maxCPUCount)
	}

	// Validate memory
	if resources.MemoryMB < minMemoryMB {
		return fmt.Errorf("memory must be at least %d MB", minMemoryMB)
	}
	if resources.MemoryMB > maxMemoryMB {
		return fmt.Errorf("memory must be at most %d MB (%d GB)", maxMemoryMB, maxMemoryMB/1024)
	}

	// Validate disk
	if resources.DiskMB < minDiskMB {
		return fmt.Errorf("disk must be at least %d MB (%d GB)", minDiskMB, minDiskMB/1024)
	}
	if resources.DiskMB > maxDiskMB {
		return fmt.Errorf("disk must be at most %d MB (%d GB)", maxDiskMB, maxDiskMB/1024)
	}

	return nil
}

// ValidateAgentConfig validates a complete agent configuration.
func ValidateAgentConfig(config api.AgentConfig) error {
	// Validate agent name
	if err := ValidateAgentName(config.Name); err != nil {
		return fmt.Errorf("invalid name: %w", err)
	}

	// Validate agent ID if provided
	if config.ID != "" {
		if err := ValidateAgentID(config.ID); err != nil {
			return fmt.Errorf("invalid ID: %w", err)
		}
	}

	// Validate tenant ID if provided
	if config.TenantID != "" {
		if err := ValidateTenantID(config.TenantID); err != nil {
			return fmt.Errorf("invalid tenant ID: %w", err)
		}
	}

	// Validate image
	if err := ValidateImage(config.Image); err != nil {
		return fmt.Errorf("invalid image: %w", err)
	}

	// Validate resources
	if err := ValidateResources(config.Resources); err != nil {
		return fmt.Errorf("invalid resources: %w", err)
	}

	return nil
}

// ValidateBootArgs validates VM boot arguments to prevent injection attacks.
func ValidateBootArgs(bootArgs string) error {
	// Disallow potentially dangerous characters
	dangerousChars := []string{";", "|", "&", "`", "$", "(", ")", "<", ">", "\n", "\r"}
	for _, char := range dangerousChars {
		if strings.Contains(bootArgs, char) {
			return fmt.Errorf("boot arguments contain dangerous character: %s", char)
		}
	}

	// Limit length to prevent abuse
	if len(bootArgs) > 512 {
		return fmt.Errorf("boot arguments too long (max 512 characters)")
	}

	return nil
}

// ValidateEnvironmentVariable validates an environment variable key=value pair.
func ValidateEnvironmentVariable(envVar string) error {
	parts := strings.SplitN(envVar, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("environment variable must be in KEY=VALUE format")
	}

	key := parts[0]
	value := parts[1]

	// Validate key: alphanumeric and underscores only
	keyRegex := regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	if !keyRegex.MatchString(key) {
		return fmt.Errorf("environment variable key must start with letter or underscore, and contain only uppercase letters, numbers, and underscores")
	}

	// Limit value length
	if len(value) > 4096 {
		return fmt.Errorf("environment variable value too long (max 4096 characters)")
	}

	// Check for common secrets in variable names (warn, don't block)
	secretKeywords := []string{"PASSWORD", "SECRET", "TOKEN", "KEY", "CREDENTIALS"}
	for _, keyword := range secretKeywords {
		if strings.Contains(key, keyword) {
			// In production, log a warning or use a secret manager
			// For now, we just allow it but this should be flagged
			break
		}
	}

	return nil
}
