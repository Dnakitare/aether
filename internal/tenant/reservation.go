// Package tenant provides multi-tenant resource management.
package tenant

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// ReservationManager manages resource reservations.
type ReservationManager struct {
	logger *slog.Logger
	mu     sync.RWMutex

	// Active reservations by ID
	reservations map[string]*Reservation

	// Reservations by tenant
	tenantReservations map[api.TenantID][]string
}

// Reservation represents a resource reservation.
type Reservation struct {
	ID          string
	TenantID    api.TenantID
	AgentID     api.AgentID
	Resources   ResourceRequest
	Status      ReservationStatus
	CreatedAt   time.Time
	ExpiresAt   time.Time
	FulfilledAt *time.Time
}

// ReservationStatus represents the state of a reservation.
type ReservationStatus string

const (
	// ReservationPending means reservation is waiting to be fulfilled.
	ReservationPending ReservationStatus = "pending"

	// ReservationFulfilled means resources have been allocated.
	ReservationFulfilled ReservationStatus = "fulfilled"

	// ReservationExpired means reservation expired before fulfillment.
	ReservationExpired ReservationStatus = "expired"

	// ReservationCanceled means reservation was canceled.
	ReservationCanceled ReservationStatus = "canceled"
)

// NewReservationManager creates a new reservation manager.
func NewReservationManager(logger *slog.Logger) *ReservationManager {
	return &ReservationManager{
		logger:             logger.With("component", "reservation_manager"),
		reservations:       make(map[string]*Reservation),
		tenantReservations: make(map[api.TenantID][]string),
	}
}

// CreateReservation creates a new resource reservation.
func (rm *ReservationManager) CreateReservation(ctx context.Context, req ReservationRequest) (*Reservation, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if req.TenantID == "" {
		return nil, fmt.Errorf("tenant ID is required")
	}

	if req.TTL == 0 {
		req.TTL = 5 * time.Minute // Default TTL
	}

	now := time.Now()
	reservation := &Reservation{
		ID:        generateReservationID(),
		TenantID:  req.TenantID,
		AgentID:   req.AgentID,
		Resources: req.Resources,
		Status:    ReservationPending,
		CreatedAt: now,
		ExpiresAt: now.Add(req.TTL),
	}

	rm.reservations[reservation.ID] = reservation

	// Track by tenant
	rm.tenantReservations[req.TenantID] = append(
		rm.tenantReservations[req.TenantID],
		reservation.ID,
	)

	rm.logger.InfoContext(ctx,
		"reservation created",
		"id", reservation.ID,
		"tenant_id", req.TenantID,
		"agent_id", req.AgentID,
		"expires_at", reservation.ExpiresAt,
	)

	return reservation, nil
}

// FulfillReservation marks a reservation as fulfilled.
func (rm *ReservationManager) FulfillReservation(ctx context.Context, reservationID string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	reservation, exists := rm.reservations[reservationID]
	if !exists {
		return fmt.Errorf("reservation %s not found", reservationID)
	}

	if reservation.Status != ReservationPending {
		return fmt.Errorf("reservation %s is not pending (status: %s)", reservationID, reservation.Status)
	}

	if time.Now().After(reservation.ExpiresAt) {
		reservation.Status = ReservationExpired
		return fmt.Errorf("reservation %s has expired", reservationID)
	}

	now := time.Now()
	reservation.Status = ReservationFulfilled
	reservation.FulfilledAt = &now

	rm.logger.InfoContext(ctx,
		"reservation fulfilled",
		"id", reservationID,
		"tenant_id", reservation.TenantID,
	)

	return nil
}

// CancelReservation cancels a reservation.
func (rm *ReservationManager) CancelReservation(ctx context.Context, reservationID string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	reservation, exists := rm.reservations[reservationID]
	if !exists {
		return fmt.Errorf("reservation %s not found", reservationID)
	}

	reservation.Status = ReservationCanceled

	rm.logger.InfoContext(ctx,
		"reservation canceled",
		"id", reservationID,
		"tenant_id", reservation.TenantID,
	)

	return nil
}

// GetReservation retrieves a reservation by ID.
func (rm *ReservationManager) GetReservation(reservationID string) (*Reservation, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	reservation, exists := rm.reservations[reservationID]
	if !exists {
		return nil, fmt.Errorf("reservation %s not found", reservationID)
	}

	return reservation, nil
}

// ListReservations returns all reservations for a tenant.
func (rm *ReservationManager) ListReservations(tenantID api.TenantID) ([]*Reservation, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	ids, exists := rm.tenantReservations[tenantID]
	if !exists {
		return []*Reservation{}, nil
	}

	reservations := make([]*Reservation, 0, len(ids))
	for _, id := range ids {
		if reservation, exists := rm.reservations[id]; exists {
			reservations = append(reservations, reservation)
		}
	}

	return reservations, nil
}

// CleanupExpired removes expired reservations.
func (rm *ReservationManager) CleanupExpired(ctx context.Context) int {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for id, reservation := range rm.reservations {
		if reservation.Status == ReservationPending && now.After(reservation.ExpiresAt) {
			reservation.Status = ReservationExpired
			cleaned++
			rm.logger.DebugContext(ctx, "reservation expired", "id", id)
		}
	}

	return cleaned
}

// ReservationRequest represents a request to create a reservation.
type ReservationRequest struct {
	TenantID  api.TenantID
	AgentID   api.AgentID
	Resources ResourceRequest
	TTL       time.Duration
}

// generateReservationID generates a unique reservation ID.
func generateReservationID() string {
	return fmt.Sprintf("rsv-%d", time.Now().UnixNano())
}
