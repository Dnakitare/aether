// Package api provides the HTTP API server.
package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/internal/scaler"
	"github.com/aether-runtime/aether/internal/tenant"
	"github.com/aether-runtime/aether/pkg/api"
)

// Health check handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	// Check if all components are ready
	// For now, simple OK response
	s.respondJSON(w, http.StatusOK, map[string]string{
		"status": "ready",
	})
}

// Agent handlers

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract tenant ID from auth context
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	// List only agents belonging to this tenant
	// TODO: Add filtering by tenant in runtime.ListAgents
	// For now, we list all and filter manually
	allAgents, err := s.runtime.ListAgents(ctx, nil)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Filter by tenant
	var agents []*api.AgentInfo
	for _, agent := range allAgents {
		if agent.Config.TenantID == tenantID {
			agents = append(agents, agent)
		}
	}

	s.respondJSON(w, http.StatusOK, agents)
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req api.AgentConfig
	if err := s.parseJSON(r, &req); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate agent configuration using validation framework
	if err := ValidateAgentConfig(req); err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("validation failed: %v", err))
		return
	}

	// Generate ID if not provided
	if req.ID == "" {
		req.ID = api.AgentID(generateID("agent"))
	}

	// Extract tenant ID from auth context and enforce it
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	// If tenant ID is provided in request, verify it matches auth context
	if req.TenantID != "" && req.TenantID != tenantID {
		s.logger.WarnContext(ctx, "tenant ID mismatch in create request",
			"auth_tenant", tenantID,
			"request_tenant", req.TenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondError(w, http.StatusForbidden, "cannot create agent for different tenant")
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
			s.respondError(w, http.StatusForbidden, err.Error())
			return
		}

		// Allocate resources
		if err := s.quotaManager.AllocateResources(ctx, req.TenantID, resources); err != nil {
			s.respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Create agent
	if err := s.runtime.CreateAgent(ctx, req); err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Start agent
	if err := s.runtime.StartAgent(ctx, req.ID); err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Get agent info
	info, err := s.runtime.GetAgent(ctx, req.ID)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
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
		s.respondError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
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
		s.respondError(w, http.StatusForbidden, "access denied")
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
		s.respondError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	// Get agent info for resource cleanup
	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
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
		s.respondError(w, http.StatusForbidden, "access denied")
		return
	}

	// Destroy agent
	if err := s.runtime.DestroyAgent(ctx, agentID); err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
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
		s.respondError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	// Verify tenant ownership
	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	if info.Config.TenantID != tenantID {
		s.logger.WarnContext(ctx, "tenant isolation violation attempt on logs",
			"requested_agent", agentID,
			"agent_tenant", info.Config.TenantID,
			"requester_tenant", tenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondError(w, http.StatusForbidden, "access denied")
		return
	}

	follow := r.URL.Query().Get("follow") == "true"

	reader, err := s.runtime.GetAgentLogs(ctx, agentID, follow)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer reader.Close()

	// Stream logs to response
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	// TODO: Implement proper streaming with SSE or WebSocket
	// For now, this is a simplified version
}

func (s *Server) handleGetAgentHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

	// Extract tenant ID from auth context
	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	// Verify tenant ownership
	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	if info.Config.TenantID != tenantID {
		s.logger.WarnContext(ctx, "tenant isolation violation attempt on health check",
			"requested_agent", agentID,
			"agent_tenant", info.Config.TenantID,
			"requester_tenant", tenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondError(w, http.StatusForbidden, "access denied")
		return
	}

	health, err := s.runtime.GetAgentHealth(ctx, agentID)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, health)
}

// Quota handlers

func (s *Server) handleListQuotas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.quotaManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "quota manager not available")
		return
	}

	// Extract claims from auth context
	claims, ok := auth.GetClaims(ctx)
	if !ok {
		s.respondError(w, http.StatusUnauthorized, "no claims in context")
		return
	}

	// Only admins can list all quotas
	if !auth.IsAdmin(claims) {
		s.respondError(w, http.StatusForbidden, "admin access required")
		return
	}

	quotas := s.quotaManager.ListQuotas()
	s.respondJSON(w, http.StatusOK, quotas)
}

func (s *Server) handleGetQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.quotaManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "quota manager not available")
		return
	}

	vars := mux.Vars(r)
	requestedTenantID := api.TenantID(vars["tenant_id"])

	// Extract tenant ID from auth context
	authTenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	// Tenants can only view their own quota unless they're an admin
	claims, ok := auth.GetClaims(ctx)
	if !ok {
		s.respondError(w, http.StatusUnauthorized, "no claims in context")
		return
	}

	if requestedTenantID != authTenantID && !auth.IsAdmin(claims) {
		s.logger.WarnContext(ctx, "tenant isolation violation on quota access",
			"requested_tenant", requestedTenantID,
			"auth_tenant", authTenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondError(w, http.StatusForbidden, "access denied")
		return
	}

	quota, err := s.quotaManager.GetQuota(requestedTenantID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, quota)
}

func (s *Server) handleSetQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.quotaManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "quota manager not available")
		return
	}

	// Only admins can set quotas
	claims, ok := auth.GetClaims(ctx)
	if !ok {
		s.respondError(w, http.StatusUnauthorized, "no claims in context")
		return
	}

	if !auth.IsAdmin(claims) {
		s.respondError(w, http.StatusForbidden, "admin access required to set quotas")
		return
	}

	vars := mux.Vars(r)
	tenantID := api.TenantID(vars["tenant_id"])

	var quota tenant.Quota
	if err := s.parseJSON(r, &quota); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	quota.TenantID = tenantID

	if err := s.quotaManager.SetQuota(&quota); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, &quota)
}

func (s *Server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.quotaManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "quota manager not available")
		return
	}

	vars := mux.Vars(r)
	requestedTenantID := api.TenantID(vars["tenant_id"])

	// Extract tenant ID from auth context
	authTenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondError(w, http.StatusUnauthorized, "no tenant in context")
		return
	}

	// Tenants can only view their own usage unless they're an admin
	claims, ok := auth.GetClaims(ctx)
	if !ok {
		s.respondError(w, http.StatusUnauthorized, "no claims in context")
		return
	}

	if requestedTenantID != authTenantID && !auth.IsAdmin(claims) {
		s.logger.WarnContext(ctx, "tenant isolation violation on usage access",
			"requested_tenant", requestedTenantID,
			"auth_tenant", authTenantID,
			"remote_addr", r.RemoteAddr,
		)
		s.respondError(w, http.StatusForbidden, "access denied")
		return
	}

	usage, err := s.quotaManager.GetUsage(requestedTenantID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, usage)
}

// Scheduler handlers

func (s *Server) handleGetSchedulerStats(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		s.respondError(w, http.StatusServiceUnavailable, "scheduler not available")
		return
	}

	stats := s.scheduler.GetStats()
	s.respondJSON(w, http.StatusOK, stats)
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		s.respondError(w, http.StatusServiceUnavailable, "scheduler not available")
		return
	}

	nodes := s.scheduler.ListNodes()
	s.respondJSON(w, http.StatusOK, nodes)
}

// Scaler handlers

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	if s.scaler == nil {
		s.respondError(w, http.StatusServiceUnavailable, "scaler not available")
		return
	}

	policies := s.scaler.ListPolicies()
	s.respondJSON(w, http.StatusOK, policies)
}

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	if s.scaler == nil {
		s.respondError(w, http.StatusServiceUnavailable, "scaler not available")
		return
	}

	var policy scaler.Policy
	if err := s.parseJSON(r, &policy); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.scaler.AddPolicy(&policy); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.respondJSON(w, http.StatusCreated, &policy)
}

func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	if s.scaler == nil {
		s.respondError(w, http.StatusServiceUnavailable, "scaler not available")
		return
	}

	vars := mux.Vars(r)
	name := vars["name"]

	policy, exists := s.scaler.GetPolicy(name)
	if !exists {
		s.respondError(w, http.StatusNotFound, "policy not found")
		return
	}

	s.respondJSON(w, http.StatusOK, policy)
}

func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	if s.scaler == nil {
		s.respondError(w, http.StatusServiceUnavailable, "scaler not available")
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
