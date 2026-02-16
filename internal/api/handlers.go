// Package api provides the HTTP API server.
package api

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/internal/scaler"
	"github.com/aether-runtime/aether/internal/tenant"
	"github.com/aether-runtime/aether/pkg/api"
)

// Agent handlers

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

	// List all agents and filter by tenant
	// NOTE: For large deployments, consider adding tenant filtering to runtime.ListAgents
	// to avoid loading all agents into memory. Current approach is sufficient for <10k agents.
	allAgents, err := s.runtime.ListAgents(ctx, nil)
	if err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to list agents: %v", err))
		return
	}

	// Filter by tenant to enforce isolation
	var agents []*api.AgentInfo
	for _, agent := range allAgents {
		if agent.Config.TenantID == tenantID {
			agents = append(agents, agent)
		}
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

	// Check quota
	if s.quotaManager != nil {
		resources := tenant.ResourceRequest{
			AgentCount: 1,
			CPUCores:   int64(req.Resources.CPUCount) * 1000,
			MemoryMB:   req.Resources.MemoryMB,
			DiskMB:     req.Resources.DiskMB,
		}

		if err := s.quotaManager.CheckQuota(ctx, req.TenantID, resources); err != nil {
			// Check if it's a quota exceeded error
			s.respondQuotaExceeded(w, "agent", 0, 0) // TODO: extract actual limits from error
			return
		}

		// Allocate resources
		if err := s.quotaManager.AllocateResources(ctx, req.TenantID, resources); err != nil {
			s.respondInternalError(w, fmt.Sprintf("Failed to allocate resources: %v", err))
			return
		}
	}

	// Create agent
	if err := s.runtime.CreateAgent(ctx, req); err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to create agent: %v", err))
		return
	}

	// Start agent
	if err := s.runtime.StartAgent(ctx, req.ID); err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to start agent: %v", err))
		return
	}

	// Get agent info
	info, err := s.runtime.GetAgent(ctx, req.ID)
	if err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to retrieve agent info: %v", err))
		return
	}

	s.respondJSON(w, http.StatusCreated, info)
}

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

	// Release resources
	if s.quotaManager != nil {
		resources := tenant.ResourceRequest{
			AgentCount: 1,
			CPUCores:   int64(info.Config.Resources.CPUCount) * 1000,
			MemoryMB:   info.Config.Resources.MemoryMB,
			DiskMB:     info.Config.Resources.DiskMB,
		}

		if err := s.quotaManager.ReleaseResources(ctx, info.Config.TenantID, resources); err != nil {
			s.logger.WarnContext(ctx, "failed to release resources", "error", err)
		}
	}

	s.respondJSON(w, http.StatusNoContent, nil)
}

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
		// io.Copy happens in runtime.GetAgentLogs
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
	for scanner.Scan() {
		line := scanner.Text()

		// Send as SSE event
		fmt.Fprintf(w, "data: %s\n\n", line)

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
}

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

	if err := s.quotaManager.SetQuota(&quota); err != nil {
		s.respondValidationError(w, "Invalid quota configuration", []FieldError{
			{Field: "quota", Message: err.Error(), Code: "INVALID_QUOTA"},
		})
		return
	}

	s.respondJSON(w, http.StatusOK, &quota)
}

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

func (s *Server) handleGetSchedulerStats(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		s.respondServiceUnavailable(w, "Scheduler")
		return
	}

	stats := s.scheduler.GetStats()
	s.respondJSON(w, http.StatusOK, stats)
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		s.respondServiceUnavailable(w, "Scheduler")
		return
	}

	nodes := s.scheduler.ListNodes()
	s.respondJSON(w, http.StatusOK, nodes)
}

// Scaler handlers

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	if s.scaler == nil {
		s.respondServiceUnavailable(w, "Scaler")
		return
	}

	policies := s.scaler.ListPolicies()
	s.respondJSON(w, http.StatusOK, policies)
}

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
	return prefix + "-" + time.Now().Format("20060102150405")
}
