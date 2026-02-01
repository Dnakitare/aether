// Package api provides the HTTP API server.
package api

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/dnakitare/aether/internal/scaler"
	"github.com/dnakitare/aether/internal/tenant"
	"github.com/dnakitare/aether/pkg/api"
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

	// TODO: Extract tenant ID from auth context
	// For now, list all agents
	agents, err := s.runtime.ListAgents(ctx, nil)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
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

	// Validate request
	if req.Name == "" {
		s.respondError(w, http.StatusBadRequest, "agent name is required")
		return
	}
	if req.Image == "" {
		s.respondError(w, http.StatusBadRequest, "agent image is required")
		return
	}

	// Generate ID if not provided
	if req.ID == "" {
		req.ID = api.AgentID(generateID("agent"))
	}

	// Set default tenant if not provided
	if req.TenantID == "" {
		// TODO: Extract from auth context
		req.TenantID = api.TenantID("default")
	}

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

	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, info)
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

	// Get agent info for resource cleanup
	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
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

	health, err := s.runtime.GetAgentHealth(ctx, agentID)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, health)
}

// Quota handlers

func (s *Server) handleListQuotas(w http.ResponseWriter, r *http.Request) {
	if s.quotaManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "quota manager not available")
		return
	}

	quotas := s.quotaManager.ListQuotas()
	s.respondJSON(w, http.StatusOK, quotas)
}

func (s *Server) handleGetQuota(w http.ResponseWriter, r *http.Request) {
	if s.quotaManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "quota manager not available")
		return
	}

	vars := mux.Vars(r)
	tenantID := api.TenantID(vars["tenant_id"])

	quota, err := s.quotaManager.GetQuota(tenantID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, quota)
}

func (s *Server) handleSetQuota(w http.ResponseWriter, r *http.Request) {
	if s.quotaManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "quota manager not available")
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
	if s.quotaManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "quota manager not available")
		return
	}

	vars := mux.Vars(r)
	tenantID := api.TenantID(vars["tenant_id"])

	usage, err := s.quotaManager.GetUsage(tenantID)
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
