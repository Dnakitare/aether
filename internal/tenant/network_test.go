package tenant_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/aether-runtime/aether/internal/tenant"
	"github.com/aether-runtime/aether/pkg/api"
)

func TestNetworkIsolation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	nim, err := tenant.NewNetworkIsolationManager(logger, "10.0.0.0/16")
	if err != nil {
		t.Fatalf("Failed to create network isolation manager: %v", err)
	}

	ctx := context.Background()

	// Create tenant network
	network, err := nim.CreateTenantNetwork(ctx, api.TenantID("tenant-1"))
	if err != nil {
		t.Fatalf("Failed to create tenant network: %v", err)
	}

	if network.Subnet == nil {
		t.Error("Expected subnet to be allocated")
	}

	if network.Gateway == nil {
		t.Error("Expected gateway to be set")
	}

	if len(network.FirewallRules) == 0 {
		t.Error("Expected default firewall rules")
	}

	// Get tenant network
	retrieved, err := nim.GetTenantNetwork(api.TenantID("tenant-1"))
	if err != nil {
		t.Fatalf("Failed to get tenant network: %v", err)
	}

	if retrieved.TenantID != network.TenantID {
		t.Errorf("TenantID = %s, want %s", retrieved.TenantID, network.TenantID)
	}

	// Add firewall rule
	rule := &tenant.FirewallRule{
		ID:        "allow-ssh",
		Action:    tenant.FirewallAllow,
		Protocol:  "tcp",
		DestPort:  22,
		Direction: tenant.DirectionInbound,
		Priority:  200,
	}

	if err := nim.AddFirewallRule(ctx, network.TenantID, rule); err != nil {
		t.Fatalf("Failed to add firewall rule: %v", err)
	}

	// Verify rule was added
	updated, err := nim.GetTenantNetwork(network.TenantID)
	if err != nil {
		t.Fatalf("Failed to get updated network: %v", err)
	}

	found := false
	for _, r := range updated.FirewallRules {
		if r.ID == "allow-ssh" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Firewall rule was not added")
	}

	// Delete tenant network
	if err := nim.DeleteTenantNetwork(ctx, network.TenantID); err != nil {
		t.Fatalf("Failed to delete tenant network: %v", err)
	}
}

func TestFirewallRuleValidation(t *testing.T) {
	tests := []struct {
		name    string
		rule    *tenant.FirewallRule
		wantErr bool
	}{
		{
			name: "valid rule",
			rule: &tenant.FirewallRule{
				ID:        "test-rule",
				Action:    tenant.FirewallAllow,
				Protocol:  "tcp",
				Direction: tenant.DirectionInbound,
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			rule: &tenant.FirewallRule{
				Action:    tenant.FirewallAllow,
				Protocol:  "tcp",
				Direction: tenant.DirectionInbound,
			},
			wantErr: true,
		},
		{
			name: "invalid action",
			rule: &tenant.FirewallRule{
				ID:        "test-rule",
				Action:    "invalid",
				Protocol:  "tcp",
				Direction: tenant.DirectionInbound,
			},
			wantErr: true,
		},
		{
			name: "invalid protocol",
			rule: &tenant.FirewallRule{
				ID:        "test-rule",
				Action:    tenant.FirewallAllow,
				Protocol:  "invalid",
				Direction: tenant.DirectionInbound,
			},
			wantErr: true,
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	nim, _ := tenant.NewNetworkIsolationManager(logger, "10.0.0.0/16")
	ctx := context.Background()

	// Create a network first
	network, _ := nim.CreateTenantNetwork(ctx, api.TenantID("test-tenant"))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := nim.AddFirewallRule(ctx, network.TenantID, tt.rule)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddFirewallRule() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
