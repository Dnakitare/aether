package tenant_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
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

// newTestLogger returns a silent logger suitable for use in tests.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

// newTestQuotaManager creates a QuotaManager with a pre-configured quota for
// the given tenantID. It is a convenience helper so individual tests stay
// focused on what they are actually testing.
func newTestQuotaManager(t *testing.T, tenantID api.TenantID, maxAgents int) *tenant.QuotaManager {
	t.Helper()
	qm := tenant.NewQuotaManager(newTestLogger(), nil)
	err := qm.SetQuota(context.Background(), &tenant.Quota{
		TenantID:    tenantID,
		MaxAgents:   maxAgents,
		MaxCPUCores: 10000,
		MaxMemoryMB: 20480,
		Tier:        "pro",
	})
	if err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	return qm
}

// TestCheckAndAllocate_AtomicRejectOnExceed verifies that when the quota is
// already at its limit, CheckAndAllocate returns a *QuotaExceededError and
// leaves usage unchanged (no partial allocation).
func TestCheckAndAllocate_AtomicRejectOnExceed(t *testing.T) {
	t.Parallel()

	const tenantID = api.TenantID("tenant-atomic-reject")
	ctx := context.Background()
	qm := newTestQuotaManager(t, tenantID, 2)

	// Fill the quota to its limit.
	fill := tenant.ResourceRequest{AgentCount: 2}
	if err := qm.CheckAndAllocate(ctx, tenantID, fill); err != nil {
		t.Fatalf("initial allocation failed: %v", err)
	}

	// Attempt to allocate one more agent beyond the limit.
	err := qm.CheckAndAllocate(ctx, tenantID, tenant.ResourceRequest{AgentCount: 1})

	var qErr *tenant.QuotaExceededError
	if !errors.As(err, &qErr) {
		t.Fatalf("expected *QuotaExceededError, got %T: %v", err, err)
	}

	// Usage must still be 2 — the failed allocation must not have been committed.
	usage, getErr := qm.GetUsage(tenantID)
	if getErr != nil {
		t.Fatalf("GetUsage: %v", getErr)
	}
	if usage.AgentCount != 2 {
		t.Errorf("AgentCount = %d after rejected allocation, want 2 (allocation must not have been applied)", usage.AgentCount)
	}
}

// TestCheckAndAllocate_AtomicSuccessUpdatesUsage verifies that a successful
// CheckAndAllocate call increments usage by exactly the requested amount.
func TestCheckAndAllocate_AtomicSuccessUpdatesUsage(t *testing.T) {
	t.Parallel()

	const tenantID = api.TenantID("tenant-atomic-success")
	ctx := context.Background()
	qm := newTestQuotaManager(t, tenantID, 5)

	req := tenant.ResourceRequest{
		AgentCount: 2,
		CPUCores:   500,
		MemoryMB:   1024,
	}

	if err := qm.CheckAndAllocate(ctx, tenantID, req); err != nil {
		t.Fatalf("CheckAndAllocate: %v", err)
	}

	usage, err := qm.GetUsage(tenantID)
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}

	if usage.AgentCount != req.AgentCount {
		t.Errorf("AgentCount = %d, want %d", usage.AgentCount, req.AgentCount)
	}
	if usage.CPUCores != req.CPUCores {
		t.Errorf("CPUCores = %d, want %d", usage.CPUCores, req.CPUCores)
	}
	if usage.MemoryMB != req.MemoryMB {
		t.Errorf("MemoryMB = %d, want %d", usage.MemoryMB, req.MemoryMB)
	}
}

// TestReleaseResources_DecrementsUsage verifies that releasing previously
// allocated resources reduces usage by the exact released amount.
func TestReleaseResources_DecrementsUsage(t *testing.T) {
	t.Parallel()

	const tenantID = api.TenantID("tenant-release-decrement")
	ctx := context.Background()
	qm := newTestQuotaManager(t, tenantID, 5)

	alloc := tenant.ResourceRequest{
		AgentCount: 3,
		CPUCores:   3000,
		MemoryMB:   6144,
	}
	if err := qm.AllocateResources(ctx, tenantID, alloc); err != nil {
		t.Fatalf("AllocateResources: %v", err)
	}

	release := tenant.ResourceRequest{
		AgentCount: 2,
		CPUCores:   1000,
		MemoryMB:   2048,
	}
	if err := qm.ReleaseResources(ctx, tenantID, release); err != nil {
		t.Fatalf("ReleaseResources: %v", err)
	}

	usage, err := qm.GetUsage(tenantID)
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}

	wantAgents := alloc.AgentCount - release.AgentCount
	wantCPU := alloc.CPUCores - release.CPUCores
	wantMem := alloc.MemoryMB - release.MemoryMB

	if usage.AgentCount != wantAgents {
		t.Errorf("AgentCount = %d, want %d", usage.AgentCount, wantAgents)
	}
	if usage.CPUCores != wantCPU {
		t.Errorf("CPUCores = %d, want %d", usage.CPUCores, wantCPU)
	}
	if usage.MemoryMB != wantMem {
		t.Errorf("MemoryMB = %d, want %d", usage.MemoryMB, wantMem)
	}
}

// TestReleaseResources_UnderflowProtection verifies that releasing more
// resources than were allocated clamps usage to zero, not negative.
func TestReleaseResources_UnderflowProtection(t *testing.T) {
	t.Parallel()

	const tenantID = api.TenantID("tenant-underflow")
	ctx := context.Background()
	qm := newTestQuotaManager(t, tenantID, 5)

	// Allocate a small amount, then release far more.
	if err := qm.AllocateResources(ctx, tenantID, tenant.ResourceRequest{
		AgentCount: 1,
		CPUCores:   500,
		MemoryMB:   512,
	}); err != nil {
		t.Fatalf("AllocateResources: %v", err)
	}

	if err := qm.ReleaseResources(ctx, tenantID, tenant.ResourceRequest{
		AgentCount: 100,
		CPUCores:   99999,
		MemoryMB:   99999,
	}); err != nil {
		t.Fatalf("ReleaseResources: %v", err)
	}

	usage, err := qm.GetUsage(tenantID)
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}

	if usage.AgentCount < 0 {
		t.Errorf("AgentCount = %d, want >= 0 (underflow not clamped)", usage.AgentCount)
	}
	if usage.CPUCores < 0 {
		t.Errorf("CPUCores = %d, want >= 0 (underflow not clamped)", usage.CPUCores)
	}
	if usage.MemoryMB < 0 {
		t.Errorf("MemoryMB = %d, want >= 0 (underflow not clamped)", usage.MemoryMB)
	}

	if usage.AgentCount != 0 {
		t.Errorf("AgentCount = %d, want 0", usage.AgentCount)
	}
	if usage.CPUCores != 0 {
		t.Errorf("CPUCores = %d, want 0", usage.CPUCores)
	}
	if usage.MemoryMB != 0 {
		t.Errorf("MemoryMB = %d, want 0", usage.MemoryMB)
	}
}

// TestConcurrentAllocations verifies that CheckAndAllocate is safe under
// concurrent load: with a quota of MaxAgents=5 and 10 competing goroutines
// each requesting 1 agent, exactly 5 succeed and 5 fail, with final usage
// equal to 5.
func TestConcurrentAllocations(t *testing.T) {
	t.Parallel()

	const tenantID = api.TenantID("tenant-concurrent")
	const quota = 5
	const goroutines = 10

	ctx := context.Background()
	qm := newTestQuotaManager(t, tenantID, quota)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		failed    int
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			err := qm.CheckAndAllocate(ctx, tenantID, tenant.ResourceRequest{AgentCount: 1})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				succeeded++
			} else {
				var qErr *tenant.QuotaExceededError
				if !errors.As(err, &qErr) {
					t.Errorf("unexpected error type %T: %v", err, err)
				}
				failed++
			}
		}()
	}
	wg.Wait()

	if succeeded != quota {
		t.Errorf("succeeded = %d, want %d", succeeded, quota)
	}
	if failed != goroutines-quota {
		t.Errorf("failed = %d, want %d", failed, goroutines-quota)
	}

	usage, err := qm.GetUsage(tenantID)
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if usage.AgentCount != quota {
		t.Errorf("final AgentCount = %d, want %d", usage.AgentCount, quota)
	}
}

// TestMultipleTenantIsolation verifies that quota consumption for one tenant
// does not affect another, and that each tenant's limit is enforced
// independently.
func TestMultipleTenantIsolation(t *testing.T) {
	t.Parallel()

	const (
		tenant1 = api.TenantID("tenant-isolation-1")
		tenant2 = api.TenantID("tenant-isolation-2")
	)
	ctx := context.Background()

	qm := tenant.NewQuotaManager(newTestLogger(), nil)

	if err := qm.SetQuota(ctx, &tenant.Quota{
		TenantID:    tenant1,
		MaxAgents:   2,
		MaxCPUCores: 10000,
		MaxMemoryMB: 20480,
		Tier:        "free",
	}); err != nil {
		t.Fatalf("SetQuota tenant1: %v", err)
	}
	if err := qm.SetQuota(ctx, &tenant.Quota{
		TenantID:    tenant2,
		MaxAgents:   3,
		MaxCPUCores: 10000,
		MaxMemoryMB: 20480,
		Tier:        "pro",
	}); err != nil {
		t.Fatalf("SetQuota tenant2: %v", err)
	}

	// Fill both tenants to their respective limits.
	allocate := func(id api.TenantID, count int) {
		t.Helper()
		if err := qm.AllocateResources(ctx, id, tenant.ResourceRequest{AgentCount: count}); err != nil {
			t.Fatalf("AllocateResources %s: %v", id, err)
		}
	}
	allocate(tenant1, 2)
	allocate(tenant2, 3)

	// Verify each tenant's usage is independent.
	u1, err := qm.GetUsage(tenant1)
	if err != nil {
		t.Fatalf("GetUsage tenant1: %v", err)
	}
	u2, err := qm.GetUsage(tenant2)
	if err != nil {
		t.Fatalf("GetUsage tenant2: %v", err)
	}

	if u1.AgentCount != 2 {
		t.Errorf("tenant1 AgentCount = %d, want 2", u1.AgentCount)
	}
	if u2.AgentCount != 3 {
		t.Errorf("tenant2 AgentCount = %d, want 3", u2.AgentCount)
	}

	// tenant1 is at its limit of 2 — a third agent must be rejected.
	err = qm.CheckAndAllocate(ctx, tenant1, tenant.ResourceRequest{AgentCount: 1})
	var qErr *tenant.QuotaExceededError
	if !errors.As(err, &qErr) {
		t.Errorf("tenant1: expected *QuotaExceededError for third agent, got %T: %v", err, err)
	}

	// tenant2 still has capacity (limit 3, used 3) — a third agent was fine,
	// but a fourth must be rejected.
	err = qm.CheckAndAllocate(ctx, tenant2, tenant.ResourceRequest{AgentCount: 1})
	if !errors.As(err, &qErr) {
		t.Errorf("tenant2: expected *QuotaExceededError for fourth agent, got %T: %v", err, err)
	}

	// tenant1 exceeding its quota must not have changed tenant2's usage.
	u2After, err := qm.GetUsage(tenant2)
	if err != nil {
		t.Fatalf("GetUsage tenant2 (after): %v", err)
	}
	if u2After.AgentCount != 3 {
		t.Errorf("tenant2 AgentCount after tenant1 overflow = %d, want 3", u2After.AgentCount)
	}
}

// TestNoQuotaSet_AllocationFails documents the actual behavior when no quota
// has been configured for a tenant: both CheckAndAllocate and AllocateResources
// return errors because there is no usage entry to update.
func TestNoQuotaSet_AllocationFails(t *testing.T) {
	t.Parallel()

	const tenantID = api.TenantID("tenant-no-quota")
	ctx := context.Background()
	qm := tenant.NewQuotaManager(newTestLogger(), nil)

	// CheckAndAllocate must fail — no quota configured.
	err := qm.CheckAndAllocate(ctx, tenantID, tenant.ResourceRequest{AgentCount: 1})
	if err == nil {
		t.Error("CheckAndAllocate: expected error for tenant with no quota, got nil")
	}

	// AllocateResources must also fail — no usage entry exists.
	err = qm.AllocateResources(ctx, tenantID, tenant.ResourceRequest{AgentCount: 1})
	if err == nil {
		t.Error("AllocateResources: expected error for tenant with no quota, got nil")
	}

	// GetUsage must also fail — no entry was ever created.
	_, err = qm.GetUsage(tenantID)
	if err == nil {
		t.Error("GetUsage: expected error for tenant with no quota, got nil")
	}
}

// TestQuotaExceededError_Fields verifies that the QuotaExceededError returned
// on an agent-count breach carries the correct structured fields.
func TestQuotaExceededError_Fields(t *testing.T) {
	t.Parallel()

	const tenantID = api.TenantID("tenant-error-fields")
	ctx := context.Background()
	qm := newTestQuotaManager(t, tenantID, 2)

	// Use up 1 agent first so the usage is non-zero — this makes the Requested
	// field more interesting to assert.
	if err := qm.AllocateResources(ctx, tenantID, tenant.ResourceRequest{AgentCount: 1}); err != nil {
		t.Fatalf("AllocateResources: %v", err)
	}

	// Request 2 more agents when only 1 slot remains — total would be 3, limit is 2.
	err := qm.CheckAndAllocate(ctx, tenantID, tenant.ResourceRequest{AgentCount: 2})

	var qErr *tenant.QuotaExceededError
	if !errors.As(err, &qErr) {
		t.Fatalf("expected *QuotaExceededError, got %T: %v", err, err)
	}

	if qErr.TenantID != tenantID {
		t.Errorf("TenantID = %q, want %q", qErr.TenantID, tenantID)
	}
	if qErr.Resource != "agents" {
		t.Errorf("Resource = %q, want %q", qErr.Resource, "agents")
	}
	// Requested is current_usage + requested = 1 + 2 = 3.
	if qErr.Requested != 3 {
		t.Errorf("Requested = %d, want 3 (current 1 + requested 2)", qErr.Requested)
	}
	if qErr.Limit != 2 {
		t.Errorf("Limit = %d, want 2", qErr.Limit)
	}
}

// TestListQuotas verifies that ListQuotas returns an entry for every tenant
// that has had a quota configured.
func TestListQuotas(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	qm := tenant.NewQuotaManager(newTestLogger(), nil)

	tenants := []api.TenantID{"list-tenant-a", "list-tenant-b", "list-tenant-c"}
	for _, id := range tenants {
		if err := qm.SetQuota(ctx, &tenant.Quota{
			TenantID:    id,
			MaxAgents:   5,
			MaxCPUCores: 5000,
			MaxMemoryMB: 10240,
			Tier:        "free",
		}); err != nil {
			t.Fatalf("SetQuota %s: %v", id, err)
		}
	}

	listed := qm.ListQuotas()

	if len(listed) != len(tenants) {
		t.Fatalf("ListQuotas returned %d entries, want %d", len(listed), len(tenants))
	}

	seen := make(map[api.TenantID]bool, len(listed))
	for _, q := range listed {
		seen[q.TenantID] = true
	}
	for _, id := range tenants {
		if !seen[id] {
			t.Errorf("ListQuotas missing entry for tenant %s", id)
		}
	}
}
