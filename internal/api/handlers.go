// Package api provides the HTTP API server.
package api

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/dnakitare/aether/internal/auth"
	"github.com/dnakitare/aether/internal/scaler"
	"github.com/dnakitare/aether/internal/scheduler"
	"github.com/dnakitare/aether/internal/tenant"
	"github.com/dnakitare/aether/pkg/api"
)

// Agent handlers

// handleListAgents lists agents for the authenticated tenant.
//
// @Summary      List agents
// @Description  Returns a paginated list of agents for the authenticated tenant
// @Tags         agents
// @Produce      json
// @Param        page      query   int  false  "Page number (default 1)"
// @Param        page_size query   int  false  "Items per page (default 20, max 100)"
// @Success      200  {object}  PaginatedResponse
// @Failure      401  {object}  ProblemDetail
// @Failure      500  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents [get]
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract tenant ID from auth context
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// Parse pagination parameters
	pagination := parsePaginationParams(r)

	// List agents filtered by tenant at the source to avoid loading all tenants' data.
	agents, err := s.runtime.ListAgents(ctx, &tenantID)
	if err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to list agents: %v", err))
		return
	}

	// Apply pagination
	totalItems := len(agents)
	start := pagination.Offset
	end := start + pagination.PageSize

	// Bounds checking
	if start > totalItems {
		start = totalItems
	}
	if end > totalItems {
		end = totalItems
	}

	// Slice the results
	paginatedAgents := agents[start:end]

	// Return paginated response
	s.respondPaginated(w, paginatedAgents, pagination, totalItems)
}

// handleCreateAgent creates a new agent.
//
// @Summary      Create agent
// @Description  Creates and starts a new isolated agent for the authenticated tenant
// @Tags         agents
// @Accept       json
// @Produce      json
// @Param        agent  body      api.AgentConfig  true  "Agent configuration"
// @Success      201    {object}  api.AgentInfo
// @Failure      400    {object}  ProblemDetail
// @Failure      401    {object}  ProblemDetail
// @Failure      403    {object}  ProblemDetail
// @Failure      429    {object}  ProblemDetail  "Quota exceeded"
// @Failure      500    {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents [post]
func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req api.AgentConfig
	if err := s.parseJSON(r, &req); err != nil {
		s.respondValidationError(w, "Invalid request body", []FieldError{
			{Field: "body", Message: err.Error(), Code: "INVALID_JSON"},
		})
		return
	}

	// Validate agent configuration using validation framework
	if err := ValidateAgentConfig(req); err != nil {
		s.respondValidationError(w, "Agent configuration validation failed", []FieldError{
			{Field: "config", Message: err.Error(), Code: "INVALID_CONFIG"},
		})
		return
	}

	// Generate ID if not provided
	if req.ID == "" {
		req.ID = api.AgentID(generateID("agent"))
	}

	// Extract tenant ID from auth context and enforce it
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// If tenant ID is provided in request, verify it matches auth context
	if req.TenantID != "" && req.TenantID != tenantID {
		s.logger.WarnContext(ctx, "tenant ID mismatch in create request",
			"auth_tenant", tenantID,
			"request_tenant", req.TenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondForbidden(w, "Cannot create agent for a different tenant")
		return
	}

	// Set tenant ID from auth context
	req.TenantID = tenantID

	// Atomically check quota and allocate in one lock to prevent TOCTOU races
	// under concurrent agent creation for the same tenant.
	var allocatedResources *tenant.ResourceRequest

	if s.quotaManager != nil {
		resources := tenant.ResourceRequest{
			AgentCount: 1,
			CPUCores:   int64(req.Resources.CPUCount) * 1000,
			MemoryMB:   req.Resources.MemoryMB,
			DiskMB:     req.Resources.DiskMB,
		}

		if err := s.quotaManager.CheckAndAllocate(ctx, req.TenantID, resources); err != nil {
			if quotaErr, ok := err.(*tenant.QuotaExceededError); ok {
				s.respondQuotaExceeded(w, quotaErr.Resource, quotaErr.Requested, quotaErr.Limit)
			} else {
				s.respondInternalError(w, fmt.Sprintf("Quota check failed: %v", err))
			}
			return
		}
		allocatedResources = &resources
	}

	// releaseQuota rolls back the allocation; safe to call multiple times (no-op if nil).
	releaseQuota := func() {
		if allocatedResources != nil {
			if rerr := s.quotaManager.ReleaseResources(ctx, req.TenantID, *allocatedResources); rerr != nil {
				s.logger.WarnContext(ctx, "failed to rollback quota allocation", "error", rerr)
			}
			allocatedResources = nil
		}
	}

	// Create agent
	if err := s.runtime.CreateAgent(ctx, req); err != nil {
		releaseQuota()
		s.respondInternalError(w, fmt.Sprintf("Failed to create agent: %v", err))
		return
	}

	// Start agent
	if err := s.runtime.StartAgent(ctx, req.ID); err != nil {
		// Destroy the agent we just created since start failed.
		if derr := s.runtime.DestroyAgent(ctx, req.ID); derr != nil {
			s.logger.WarnContext(ctx, "failed to destroy agent after start failure",
				"agent_id", req.ID, "error", derr)
		}
		releaseQuota()
		s.respondInternalError(w, fmt.Sprintf("Failed to start agent: %v", err))
		return
	}

	// Record the allocation on the scheduler so its node resource accounting
	// stays accurate even though we bypassed the async queue.
	if s.scheduler != nil {
		s.scheduler.RecordAllocation(ctx, req.ID, req.TenantID, scheduler.FromAgentConfig(req))
	}

	// Get agent info
	info, err := s.runtime.GetAgent(ctx, req.ID)
	if err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to retrieve agent info: %v", err))
		return
	}

	s.respondJSON(w, http.StatusCreated, info)
}

// handleGetAgent retrieves a specific agent by ID.
//
// @Summary      Get agent
// @Description  Returns details for a specific agent
// @Tags         agents
// @Produce      json
// @Param        id   path      string  true  "Agent ID"
// @Success      200  {object}  api.AgentInfo
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/{id} [get]
func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

	// Extract tenant ID from auth context
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondNotFound(w, "Agent", string(agentID))
		return
	}

	// Verify tenant ownership
	if info.Config.TenantID != tenantID {
		s.logger.WarnContext(ctx, "tenant isolation violation attempt",
			"requested_agent", agentID,
			"agent_tenant", info.Config.TenantID,
			"requester_tenant", tenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondForbidden(w, "Access denied: you do not have permission to access this agent")
		return
	}

	s.respondJSON(w, http.StatusOK, info)
}

// handleDeleteAgent stops and destroys an agent.
//
// @Summary      Delete agent
// @Description  Stops and destroys an agent
// @Tags         agents
// @Produce      json
// @Param        id   path  string  true  "Agent ID"
// @Success      204
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/{id} [delete]
func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

	// Extract tenant ID from auth context
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// Get agent info for resource cleanup
	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondNotFound(w, "Agent", string(agentID))
		return
	}

	// Verify tenant ownership
	if info.Config.TenantID != tenantID {
		s.logger.WarnContext(ctx, "tenant isolation violation attempt on delete",
			"requested_agent", agentID,
			"agent_tenant", info.Config.TenantID,
			"requester_tenant", tenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondForbidden(w, "Access denied: you do not have permission to delete this agent")
		return
	}

	// Destroy agent
	if err := s.runtime.DestroyAgent(ctx, agentID); err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to destroy agent: %v", err))
		return
	}

	// Release quota and scheduler allocations.
	if s.quotaManager != nil {
		resources := tenant.ResourceRequest{
			AgentCount: 1,
			CPUCores:   int64(info.Config.Resources.CPUCount) * 1000,
			MemoryMB:   info.Config.Resources.MemoryMB,
			DiskMB:     info.Config.Resources.DiskMB,
		}

		if err := s.quotaManager.ReleaseResources(ctx, info.Config.TenantID, resources); err != nil {
			s.logger.WarnContext(ctx, "failed to release quota resources", "error", err)
		}
	}

	if s.scheduler != nil {
		s.scheduler.UnscheduleAgent(ctx, agentID)
	}

	s.respondJSON(w, http.StatusNoContent, nil)
}

// handleGetAgentLogs returns logs for an agent.
//
// @Summary      Get agent logs
// @Description  Returns logs for an agent. Use ?follow=true for streaming via Server-Sent Events.
// @Tags         agents
// @Produce      plain
// @Param        id      path   string  true   "Agent ID"
// @Param        follow  query  bool    false  "Stream logs"
// @Success      200  {string}  string  "Log output"
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/{id}/logs [get]
func (s *Server) handleGetAgentLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

	// Extract tenant ID from auth context
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// Verify tenant ownership
	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondNotFound(w, "Agent", string(agentID))
		return
	}

	if info.Config.TenantID != tenantID {
		s.logger.WarnContext(ctx, "tenant isolation violation attempt on logs",
			"requested_agent", agentID,
			"agent_tenant", info.Config.TenantID,
			"requester_tenant", tenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondForbidden(w, "Access denied: you do not have permission to access logs for this agent")
		return
	}

	follow := r.URL.Query().Get("follow") == "true"

	reader, err := s.runtime.GetAgentLogs(ctx, agentID, follow)
	if err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to retrieve agent logs: %v", err))
		return
	}
	defer reader.Close()

	// Check if client wants streaming
	acceptHeader := r.Header.Get("Accept")
	if follow && acceptHeader == "text/event-stream" {
		// Use Server-Sent Events for streaming
		s.streamLogsSSE(w, r, reader)
	} else {
		// Standard response for non-streaming or static logs
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, reader); err != nil && r.Context().Err() == nil {
			s.logger.WarnContext(r.Context(), "error copying log stream", "error", err)
		}
	}
}

// streamLogsSSE streams logs using Server-Sent Events (SSE).
func (s *Server) streamLogsSSE(w http.ResponseWriter, r *http.Request, reader io.ReadCloser) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Flush headers immediately
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Stream logs line by line
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1 MB max line
	for scanner.Scan() {
		line := scanner.Text()

		// SSE spec: multi-line data must use "data: " prefix on each line
		escaped := strings.ReplaceAll(line, "\n", "\ndata: ")

		// Send as SSE event
		fmt.Fprintf(w, "data: %s\n\n", escaped)

		// Flush after each line
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// Check if client disconnected
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
	if err := scanner.Err(); err != nil && r.Context().Err() == nil {
		s.logger.WarnContext(r.Context(), "log scanner error", "error", err)
	}
}

// handleGetAgentHealth returns the health status for an agent.
//
// @Summary      Get agent health
// @Description  Returns health status for an agent
// @Tags         agents
// @Produce      json
// @Param        id   path      string  true  "Agent ID"
// @Success      200  {object}  api.HealthStatus
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/{id}/health [get]
func (s *Server) handleGetAgentHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

	// Extract tenant ID from auth context
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// Verify tenant ownership
	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondNotFound(w, "Agent", string(agentID))
		return
	}

	if info.Config.TenantID != tenantID {
		s.logger.WarnContext(ctx, "tenant isolation violation attempt on health check",
			"requested_agent", agentID,
			"agent_tenant", info.Config.TenantID,
			"requester_tenant", tenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondForbidden(w, "Access denied: you do not have permission to check health for this agent")
		return
	}

	health, err := s.runtime.GetAgentHealth(ctx, agentID)
	if err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to retrieve agent health: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, health)
}

// Quota handlers

// handleListQuotas lists all tenant quotas (admin only).
//
// @Summary      List quotas
// @Description  Returns all tenant quotas. Requires admin role.
// @Tags         quotas
// @Produce      json
// @Success      200  {array}   tenant.Quota
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      503  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /quotas [get]
func (s *Server) handleListQuotas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.quotaManager == nil {
		s.respondServiceUnavailable(w, "Quota manager")
		return
	}

	// Extract claims from auth context
	claims, ok := auth.GetClaims(ctx)
	if !ok {
		s.respondUnauthorized(w, "Authentication required: no claims in context")
		return
	}

	// Only admins can list all quotas
	if !auth.IsAdmin(claims) {
		s.respondForbidden(w, "Admin access required to list all quotas")
		return
	}

	quotas := s.quotaManager.ListQuotas()
	s.respondJSON(w, http.StatusOK, quotas)
}

// handleGetQuota returns the quota for a specific tenant.
//
// @Summary      Get quota
// @Description  Returns the resource quota for a tenant. Tenants can only view their own quota unless admin.
// @Tags         quotas
// @Produce      json
// @Param        tenant_id  path      string  true  "Tenant ID"
// @Success      200  {object}  tenant.Quota
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Failure      503  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /quotas/{tenant_id} [get]
func (s *Server) handleGetQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.quotaManager == nil {
		s.respondServiceUnavailable(w, "Quota manager")
		return
	}

	vars := mux.Vars(r)
	requestedTenantID := api.TenantID(vars["tenant_id"])

	// Extract tenant ID from auth context
	authTenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// Tenants can only view their own quota unless they're an admin
	claims, ok := auth.GetClaims(ctx)
	if !ok {
		s.respondUnauthorized(w, "Authentication required: no claims in context")
		return
	}

	if requestedTenantID != authTenantID && !auth.IsAdmin(claims) {
		s.logger.WarnContext(ctx, "tenant isolation violation on quota access",
			"requested_tenant", requestedTenantID,
			"auth_tenant", authTenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondForbidden(w, "Access denied: you can only view your own quota")
		return
	}

	quota, err := s.quotaManager.GetQuota(requestedTenantID)
	if err != nil {
		s.respondNotFound(w, "Quota", string(requestedTenantID))
		return
	}

	s.respondJSON(w, http.StatusOK, quota)
}

// handleSetQuota sets the resource quota for a tenant (admin only).
//
// @Summary      Set quota
// @Description  Sets the resource quota for a tenant. Requires admin role.
// @Tags         quotas
// @Accept       json
// @Produce      json
// @Param        tenant_id  path      string        true  "Tenant ID"
// @Param        quota      body      tenant.Quota  true  "Quota configuration"
// @Success      200  {object}  tenant.Quota
// @Failure      400  {object}  ProblemDetail
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      503  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /quotas/{tenant_id} [put]
func (s *Server) handleSetQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.quotaManager == nil {
		s.respondServiceUnavailable(w, "Quota manager")
		return
	}

	// Only admins can set quotas
	claims, ok := auth.GetClaims(ctx)
	if !ok {
		s.respondUnauthorized(w, "Authentication required: no claims in context")
		return
	}

	if !auth.IsAdmin(claims) {
		s.respondForbidden(w, "Admin access required to set quotas")
		return
	}

	vars := mux.Vars(r)
	tenantID := api.TenantID(vars["tenant_id"])

	var quota tenant.Quota
	if err := s.parseJSON(r, &quota); err != nil {
		s.respondValidationError(w, "Invalid request body", []FieldError{
			{Field: "body", Message: err.Error(), Code: "INVALID_JSON"},
		})
		return
	}

	quota.TenantID = tenantID

	if err := s.quotaManager.SetQuota(ctx, &quota); err != nil {
		s.respondValidationError(w, "Invalid quota configuration", []FieldError{
			{Field: "quota", Message: err.Error(), Code: "INVALID_QUOTA"},
		})
		return
	}

	s.respondJSON(w, http.StatusOK, &quota)
}

// handleGetUsage returns current resource usage for a tenant.
//
// @Summary      Get usage
// @Description  Returns current resource usage for a tenant. Tenants can only view their own usage unless admin.
// @Tags         quotas
// @Produce      json
// @Param        tenant_id  path      string  true  "Tenant ID"
// @Success      200  {object}  tenant.ResourceUsage
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Failure      503  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /quotas/{tenant_id}/usage [get]
func (s *Server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.quotaManager == nil {
		s.respondServiceUnavailable(w, "Quota manager")
		return
	}

	vars := mux.Vars(r)
	requestedTenantID := api.TenantID(vars["tenant_id"])

	// Extract tenant ID from auth context
	authTenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// Tenants can only view their own usage unless they're an admin
	claims, ok := auth.GetClaims(ctx)
	if !ok {
		s.respondUnauthorized(w, "Authentication required: no claims in context")
		return
	}

	if requestedTenantID != authTenantID && !auth.IsAdmin(claims) {
		s.logger.WarnContext(ctx, "tenant isolation violation on usage access",
			"requested_tenant", requestedTenantID,
			"auth_tenant", authTenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondForbidden(w, "Access denied: you can only view your own usage")
		return
	}

	usage, err := s.quotaManager.GetUsage(requestedTenantID)
	if err != nil {
		s.respondNotFound(w, "Usage", string(requestedTenantID))
		return
	}

	s.respondJSON(w, http.StatusOK, usage)
}

// Scheduler handlers

// handleGetSchedulerStats returns scheduler statistics.
//
// @Summary      Get scheduler stats
// @Description  Returns current statistics from the agent scheduler
// @Tags         scheduler
// @Produce      json
// @Success      200  {object}  scheduler.Stats
// @Failure      503  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /scheduler/stats [get]
func (s *Server) handleGetSchedulerStats(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		s.respondServiceUnavailable(w, "Scheduler")
		return
	}

	stats := s.scheduler.GetStats()
	s.respondJSON(w, http.StatusOK, stats)
}

// handleListNodes lists all scheduler nodes.
//
// @Summary      List nodes
// @Description  Returns all nodes registered with the scheduler
// @Tags         scheduler
// @Produce      json
// @Success      200  {array}   scheduler.Node
// @Failure      503  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /scheduler/nodes [get]
func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		s.respondServiceUnavailable(w, "Scheduler")
		return
	}

	nodes := s.scheduler.ListNodes()
	s.respondJSON(w, http.StatusOK, nodes)
}

// Scaler handlers

// handleListPolicies lists all scaling policies.
//
// @Summary      List scaling policies
// @Description  Returns all configured auto-scaling policies
// @Tags         scaler
// @Produce      json
// @Success      200  {array}   scaler.Policy
// @Failure      503  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /scaler/policies [get]
func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	if s.scaler == nil {
		s.respondServiceUnavailable(w, "Scaler")
		return
	}

	policies := s.scaler.ListPolicies()
	s.respondJSON(w, http.StatusOK, policies)
}

// handleCreatePolicy creates a new scaling policy.
//
// @Summary      Create scaling policy
// @Description  Creates a new auto-scaling policy
// @Tags         scaler
// @Accept       json
// @Produce      json
// @Param        policy  body      scaler.Policy  true  "Scaling policy configuration"
// @Success      201  {object}  scaler.Policy
// @Failure      400  {object}  ProblemDetail
// @Failure      503  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /scaler/policies [post]
func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	if s.scaler == nil {
		s.respondServiceUnavailable(w, "Scaler")
		return
	}

	var policy scaler.Policy
	if err := s.parseJSON(r, &policy); err != nil {
		s.respondValidationError(w, "Invalid request body", []FieldError{
			{Field: "body", Message: err.Error(), Code: "INVALID_JSON"},
		})
		return
	}

	if err := s.scaler.AddPolicy(&policy); err != nil {
		s.respondValidationError(w, "Invalid policy configuration", []FieldError{
			{Field: "policy", Message: err.Error(), Code: "INVALID_POLICY"},
		})
		return
	}

	s.respondJSON(w, http.StatusCreated, &policy)
}

// handleGetPolicy retrieves a specific scaling policy by name.
//
// @Summary      Get scaling policy
// @Description  Returns a specific auto-scaling policy by name
// @Tags         scaler
// @Produce      json
// @Param        name  path      string  true  "Policy name"
// @Success      200  {object}  scaler.Policy
// @Failure      404  {object}  ProblemDetail
// @Failure      503  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /scaler/policies/{name} [get]
func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	if s.scaler == nil {
		s.respondServiceUnavailable(w, "Scaler")
		return
	}

	vars := mux.Vars(r)
	name := vars["name"]

	policy, exists := s.scaler.GetPolicy(name)
	if !exists {
		s.respondNotFound(w, "Policy", name)
		return
	}

	s.respondJSON(w, http.StatusOK, policy)
}

// handleDeletePolicy removes a scaling policy by name.
//
// @Summary      Delete scaling policy
// @Description  Removes an auto-scaling policy by name
// @Tags         scaler
// @Produce      json
// @Param        name  path  string  true  "Policy name"
// @Success      204
// @Failure      503  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /scaler/policies/{name} [delete]
func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	if s.scaler == nil {
		s.respondServiceUnavailable(w, "Scaler")
		return
	}

	vars := mux.Vars(r)
	name := vars["name"]

	s.scaler.RemovePolicy(name)
	s.respondJSON(w, http.StatusNoContent, nil)
}

// generateID generates a unique ID with a prefix.
func generateID(prefix string) string {
	return prefix + "-" + uuid.New().String()
}
