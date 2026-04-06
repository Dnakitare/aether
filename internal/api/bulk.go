// Package api provides bulk operation endpoints.
package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/dnakitare/aether/internal/auth"
	"github.com/dnakitare/aether/internal/scheduler"
	"github.com/dnakitare/aether/internal/tenant"
	"github.com/dnakitare/aether/pkg/api"
)

// BulkCreateAgentsRequest represents a bulk agent creation request.
type BulkCreateAgentsRequest struct {
	Agents []api.AgentConfig `json:"agents"`
}

// BulkCreateAgentsResponse represents the response for bulk agent creation.
type BulkCreateAgentsResponse struct {
	Created []BulkOperationResult `json:"created"`
	Failed  []BulkOperationResult `json:"failed"`
	Summary BulkOperationSummary  `json:"summary"`
}

// BulkDeleteAgentsRequest represents a bulk agent deletion request.
type BulkDeleteAgentsRequest struct {
	AgentIDs []api.AgentID `json:"agent_ids"`
}

// BulkDeleteAgentsResponse represents the response for bulk agent deletion.
type BulkDeleteAgentsResponse struct {
	Deleted []BulkOperationResult `json:"deleted"`
	Failed  []BulkOperationResult `json:"failed"`
	Summary BulkOperationSummary  `json:"summary"`
}

// BulkOperationResult represents the result of a single operation in a bulk request.
type BulkOperationResult struct {
	AgentID api.AgentID `json:"agent_id"`
	Success bool        `json:"success"`
	Error   string      `json:"error,omitempty"`
}

// BulkOperationSummary provides summary statistics for bulk operations.
type BulkOperationSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

// handleBulkCreateAgents handles bulk agent creation.
//
// @Summary      Bulk create agents
// @Description  Creates multiple agents in a single request (concurrently). Partial success is possible.
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        request  body      BulkCreateAgentsRequest  true  "Bulk create request"
// @Success      200  {object}  BulkCreateAgentsResponse
// @Failure      400  {object}  ProblemDetail
// @Failure      401  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/bulk/create [post]
func (s *Server) handleBulkCreateAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract tenant ID from auth context
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// Parse request
	var req BulkCreateAgentsRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.respondValidationError(w, "Invalid request body", []FieldError{
			{Field: "body", Message: err.Error(), Code: "INVALID_JSON"},
		})
		return
	}

	// Validate request
	if len(req.Agents) == 0 {
		s.respondValidationError(w, "No agents specified", []FieldError{
			{Field: "agents", Message: "At least one agent configuration is required", Code: "EMPTY_ARRAY"},
		})
		return
	}

	// Limit bulk operations to prevent abuse
	const maxBulkSize = 100
	if len(req.Agents) > maxBulkSize {
		s.respondValidationError(w, "Too many agents in bulk request", []FieldError{
			{Field: "agents", Message: fmt.Sprintf("Maximum %d agents allowed per bulk request", maxBulkSize), Code: "EXCEEDS_LIMIT"},
		})
		return
	}

	// Process agents concurrently
	results := s.bulkCreateAgents(ctx, tenantID, req.Agents)

	// Build response
	response := BulkCreateAgentsResponse{
		Created: []BulkOperationResult{},
		Failed:  []BulkOperationResult{},
		Summary: BulkOperationSummary{Total: len(results)},
	}

	for _, result := range results {
		if result.Success {
			response.Created = append(response.Created, result)
			response.Summary.Succeeded++
		} else {
			response.Failed = append(response.Failed, result)
			response.Summary.Failed++
		}
	}

	// Use 207 Multi-Status if there were any failures
	status := http.StatusCreated
	if response.Summary.Failed > 0 {
		status = http.StatusMultiStatus
	}

	s.respondJSON(w, status, response)
}

// bulkCreateAgents creates multiple agents concurrently.
func (s *Server) bulkCreateAgents(ctx context.Context, tenantID api.TenantID, configs []api.AgentConfig) []BulkOperationResult {
	results := make([]BulkOperationResult, len(configs))
	var wg sync.WaitGroup

	// Use a semaphore to limit concurrency
	semaphore := make(chan struct{}, 10)

	for i, config := range configs {
		wg.Add(1)
		go func(index int, cfg api.AgentConfig) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			result := BulkOperationResult{
				AgentID: cfg.ID,
				Success: false,
			}

			// Generate ID if not provided
			if cfg.ID == "" {
				cfg.ID = api.AgentID(generateID("agent"))
				result.AgentID = cfg.ID
			}

			// Enforce tenant ID
			cfg.TenantID = tenantID

			// Validate configuration
			if err := ValidateAgentConfig(cfg); err != nil {
				result.Error = fmt.Sprintf("validation failed: %v", err)
				results[index] = result
				return
			}

			// Atomically check and allocate quota before creating.
			if s.quotaManager != nil {
				resources := tenant.ResourceRequest{
					AgentCount: 1,
					CPUCores:   int64(cfg.Resources.CPUCount) * 1000,
					MemoryMB:   cfg.Resources.MemoryMB,
					DiskMB:     cfg.Resources.DiskMB,
				}
				if err := s.quotaManager.CheckAndAllocate(ctx, cfg.TenantID, resources); err != nil {
					result.Error = fmt.Sprintf("quota exceeded: %v", err)
					results[index] = result
					return
				}
				defer func() {
					if !result.Success {
						if rerr := s.quotaManager.ReleaseResources(ctx, cfg.TenantID, resources); rerr != nil {
							s.logger.WarnContext(ctx, "failed to rollback bulk quota allocation",
								"agent_id", cfg.ID, "error", rerr)
						}
					}
				}()
			}

			// Create agent
			if err := s.runtime.CreateAgent(ctx, cfg); err != nil {
				result.Error = fmt.Sprintf("create failed: %v", err)
				results[index] = result
				return
			}

			// Start agent — destroy on failure to avoid orphaned records
			if err := s.runtime.StartAgent(ctx, cfg.ID); err != nil {
				if derr := s.runtime.DestroyAgent(ctx, cfg.ID); derr != nil {
					s.logger.WarnContext(ctx, "failed to destroy agent after bulk start failure",
						"agent_id", cfg.ID, "error", derr)
				}
				if s.scheduler != nil {
					s.scheduler.UnscheduleAgent(ctx, cfg.ID)
				}
				result.Error = fmt.Sprintf("start failed: %v", err)
				results[index] = result
				return
			}

			// Record scheduler allocation and business metrics.
			if s.scheduler != nil {
				s.scheduler.RecordAllocation(ctx, cfg.ID, cfg.TenantID, scheduler.FromAgentConfig(cfg))
			}
			if s.metrics != nil {
				s.metrics.RecordAgentOperation("create", "success", cfg.TenantID)
			}

			result.Success = true
			results[index] = result
		}(i, config)
	}

	wg.Wait()
	return results
}

// handleBulkDeleteAgents handles bulk agent deletion.
//
// @Summary      Bulk delete agents
// @Description  Deletes multiple agents in a single request (concurrently). Partial success is possible.
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        request  body      BulkDeleteAgentsRequest  true  "Bulk delete request"
// @Success      200  {object}  BulkDeleteAgentsResponse
// @Failure      400  {object}  ProblemDetail
// @Failure      401  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/bulk/delete [post]
func (s *Server) handleBulkDeleteAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract tenant ID from auth context
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// Parse request
	var req BulkDeleteAgentsRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.respondValidationError(w, "Invalid request body", []FieldError{
			{Field: "body", Message: err.Error(), Code: "INVALID_JSON"},
		})
		return
	}

	// Validate request
	if len(req.AgentIDs) == 0 {
		s.respondValidationError(w, "No agent IDs specified", []FieldError{
			{Field: "agent_ids", Message: "At least one agent ID is required", Code: "EMPTY_ARRAY"},
		})
		return
	}

	// Limit bulk operations
	const maxBulkSize = 100
	if len(req.AgentIDs) > maxBulkSize {
		s.respondValidationError(w, "Too many agents in bulk request", []FieldError{
			{Field: "agent_ids", Message: fmt.Sprintf("Maximum %d agents allowed per bulk request", maxBulkSize), Code: "EXCEEDS_LIMIT"},
		})
		return
	}

	// Process deletions concurrently
	results := s.bulkDeleteAgents(ctx, tenantID, req.AgentIDs)

	// Build response
	response := BulkDeleteAgentsResponse{
		Deleted: []BulkOperationResult{},
		Failed:  []BulkOperationResult{},
		Summary: BulkOperationSummary{Total: len(results)},
	}

	for _, result := range results {
		if result.Success {
			response.Deleted = append(response.Deleted, result)
			response.Summary.Succeeded++
		} else {
			response.Failed = append(response.Failed, result)
			response.Summary.Failed++
		}
	}

	// Use 207 Multi-Status if there were any failures
	status := http.StatusOK
	if response.Summary.Failed > 0 {
		status = http.StatusMultiStatus
	}

	s.respondJSON(w, status, response)
}

// bulkDeleteAgents deletes multiple agents concurrently.
func (s *Server) bulkDeleteAgents(ctx context.Context, tenantID api.TenantID, agentIDs []api.AgentID) []BulkOperationResult {
	results := make([]BulkOperationResult, len(agentIDs))
	var wg sync.WaitGroup

	// Use a semaphore to limit concurrency
	semaphore := make(chan struct{}, 10)

	for i, agentID := range agentIDs {
		wg.Add(1)
		go func(index int, id api.AgentID) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			result := BulkOperationResult{
				AgentID: id,
				Success: false,
			}

			// Get agent to verify ownership
			info, err := s.runtime.GetAgent(ctx, id)
			if err != nil {
				result.Error = fmt.Sprintf("agent not found: %v", err)
				results[index] = result
				return
			}

			// Verify tenant ownership
			if info.Config.TenantID != tenantID {
				result.Error = "access denied: not owned by your tenant"
				results[index] = result
				return
			}

			// Delete agent
			if err := s.runtime.DestroyAgent(ctx, id); err != nil {
				result.Error = fmt.Sprintf("delete failed: %v", err)
				results[index] = result
				return
			}

			// Release quota for the destroyed agent.
			if s.quotaManager != nil {
				resources := tenant.ResourceRequest{
					AgentCount: 1,
					CPUCores:   int64(info.Config.Resources.CPUCount) * 1000,
					MemoryMB:   info.Config.Resources.MemoryMB,
					DiskMB:     info.Config.Resources.DiskMB,
				}
				if rerr := s.quotaManager.ReleaseResources(ctx, info.Config.TenantID, resources); rerr != nil {
					s.logger.WarnContext(ctx, "failed to release quota after bulk delete",
						"agent_id", id, "error", rerr)
				}
			}

			if s.scheduler != nil {
				s.scheduler.UnscheduleAgent(ctx, id)
			}

			if s.metrics != nil {
				s.metrics.RecordAgentOperation("delete", "success", info.Config.TenantID)
			}

			result.Success = true
			results[index] = result
		}(i, agentID)
	}

	wg.Wait()
	return results
}
