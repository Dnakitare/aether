package tenant_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/tenant"
	"github.com/dnakitare/aether/pkg/api"
)

func TestQuotaManager(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	qm := tenant.NewQuotaManager(logger, nil)

	// Set a quota
	quota := &tenant.Quota{
		TenantID:    api.TenantID("tenant-1"),
		MaxAgents:   10,
		MaxCPUCores: 10000,
		MaxMemoryMB: 20480,
		Tier:        "pro",
	}

	err := qm.SetQuota(ctx, quota)
	if err != nil {
		t.Fatalf("Failed to set quota: %v", err)
	}

	// Get quota
	retrieved, err := qm.GetQuota(api.TenantID("tenant-1"))
	if err != nil {
		t.Fatalf("Failed to get quota: %v", err)
	}

	if retrieved.MaxAgents != quota.MaxAgents {
		t.Errorf("MaxAgents = %d, want %d", retrieved.MaxAgents, quota.MaxAgents)
	}

	// Check quota (should pass)
	request := tenant.ResourceRequest{
		AgentCount: 2,
		CPUCores:   2000,
		MemoryMB:   4096,
	}

	err = qm.CheckQuota(ctx, api.TenantID("tenant-1"), request)
	if err != nil {
		t.Errorf("CheckQuota failed: %v", err)
	}

	// Allocate resources
	err = qm.AllocateResources(ctx, api.TenantID("tenant-1"), request)
	if err != nil {
		t.Fatalf("Failed to allocate resources: %v", err)
	}

	// Check usage
	usage, err := qm.GetUsage(api.TenantID("tenant-1"))
	if err != nil {
		t.Fatalf("Failed to get usage: %v", err)
	}

	if usage.AgentCount != request.AgentCount {
		t.Errorf("AgentCount = %d, want %d", usage.AgentCount, request.AgentCount)
	}
}

func TestQuotaExceeded(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	qm := tenant.NewQuotaManager(logger, nil)

	// Set a small quota
	quota := &tenant.Quota{
		TenantID:    api.TenantID("tenant-small"),
		MaxAgents:   2,
		MaxCPUCores: 2000,
		MaxMemoryMB: 2048,
		Tier:        "free",
	}

	err := qm.SetQuota(ctx, quota)
	if err != nil {
		t.Fatalf("Failed to set quota: %v", err)
	}

	// Try to exceed quota
	request := tenant.ResourceRequest{
		AgentCount: 5, // Exceeds MaxAgents
		CPUCores:   5000,
		MemoryMB:   5120,
	}

	err = qm.CheckQuota(ctx, api.TenantID("tenant-small"), request)
	if err == nil {
		t.Error("Expected quota exceeded error but got none")
	}
}

func TestReservationManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	rm := tenant.NewReservationManager(logger)
	ctx := context.Background()

	// Create a reservation
	req := tenant.ReservationRequest{
		TenantID: api.TenantID("tenant-1"),
		AgentID:  api.AgentID("agent-1"),
		Resources: tenant.ResourceRequest{
			AgentCount: 1,
			CPUCores:   1000,
			MemoryMB:   512,
		},
		TTL: 10 * time.Second,
	}

	reservation, err := rm.CreateReservation(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create reservation: %v", err)
	}

	if reservation.Status != tenant.ReservationPending {
		t.Errorf("Status = %s, want %s", reservation.Status, tenant.ReservationPending)
	}

	// Fulfill reservation
	err = rm.FulfillReservation(ctx, reservation.ID)
	if err != nil {
		t.Fatalf("Failed to fulfill reservation: %v", err)
	}

	// Check status
	updated, err := rm.GetReservation(reservation.ID)
	if err != nil {
		t.Fatalf("Failed to get reservation: %v", err)
	}

	if updated.Status != tenant.ReservationFulfilled {
		t.Errorf("Status = %s, want %s", updated.Status, tenant.ReservationFulfilled)
	}
}

func TestReservationExpiry(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	rm := tenant.NewReservationManager(logger)
	ctx := context.Background()

	// Create a reservation with short TTL
	req := tenant.ReservationRequest{
		TenantID: api.TenantID("tenant-1"),
		AgentID:  api.AgentID("agent-1"),
		Resources: tenant.ResourceRequest{
			AgentCount: 1,
			CPUCores:   1000,
			MemoryMB:   512,
		},
		TTL: 100 * time.Millisecond,
	}

	reservation, err := rm.CreateReservation(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create reservation: %v", err)
	}

	// Wait for expiry
	time.Sleep(150 * time.Millisecond)

	// Try to fulfill (should fail)
	err = rm.FulfillReservation(ctx, reservation.ID)
	if err == nil {
		t.Error("Expected error for expired reservation but got none")
	}
}

func TestDefaultQuotas(t *testing.T) {
	quotas := tenant.DefaultQuotas()

	tiers := []string{"free", "pro", "enterprise"}
	for _, tier := range tiers {
		quota, exists := quotas[tier]
		if !exists {
			t.Errorf("Missing default quota for tier: %s", tier)
		}
		if quota.MaxAgents <= 0 {
			t.Errorf("Invalid MaxAgents for tier %s: %d", tier, quota.MaxAgents)
		}
	}
}
