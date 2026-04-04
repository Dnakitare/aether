package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/dnakitare/aether/internal/auth"
	_ "github.com/dnakitare/aether/internal/recovery" // imported for swagger type resolution
	"github.com/dnakitare/aether/pkg/api"
)

// handleListCheckpoints lists all checkpoints for an agent.
//
// @Summary      List checkpoints
// @Description  Returns all checkpoints for a specific agent
// @Tags         checkpoints
// @Produce      json
// @Param        id   path      string  true  "Agent ID"
// @Success      200  {array}   recovery.Checkpoint
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/{id}/checkpoints [get]
func (s *Server) handleListCheckpoints(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// Verify ownership before exposing checkpoints.
	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondNotFound(w, "Agent", string(agentID))
		return
	}
	if info.Config.TenantID != tenantID {
		s.respondForbidden(w, "Access denied: you do not have permission to access this agent")
		return
	}

	checkpoints, err := s.checkpointRuntime.ListCheckpoints(ctx, agentID)
	if err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to list checkpoints: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, checkpoints)
}

// handleCreateCheckpoint creates a new checkpoint for an agent.
//
// @Summary      Create checkpoint
// @Description  Creates a state checkpoint for the specified agent
// @Tags         checkpoints
// @Produce      json
// @Param        id   path      string  true  "Agent ID"
// @Success      201  {object}  recovery.Checkpoint
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Failure      500  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/{id}/checkpoints [post]
func (s *Server) handleCreateCheckpoint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

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
	if info.Config.TenantID != tenantID {
		s.respondForbidden(w, "Access denied: you do not have permission to access this agent")
		return
	}

	checkpoint, err := s.checkpointRuntime.CreateCheckpoint(ctx, agentID)
	if err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to create checkpoint: %v", err))
		return
	}

	s.respondJSON(w, http.StatusCreated, checkpoint)
}

// handleGetLatestCheckpoint retrieves the most recent checkpoint for an agent.
//
// @Summary      Get latest checkpoint
// @Description  Returns the most recent checkpoint for a specific agent
// @Tags         checkpoints
// @Produce      json
// @Param        id   path      string  true  "Agent ID"
// @Success      200  {object}  recovery.Checkpoint
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/{id}/checkpoints/latest [get]
func (s *Server) handleGetLatestCheckpoint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

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
	if info.Config.TenantID != tenantID {
		s.respondForbidden(w, "Access denied: you do not have permission to access this agent")
		return
	}

	checkpoint, err := s.checkpointRuntime.GetLatestCheckpoint(ctx, agentID)
	if err != nil {
		s.respondNotFound(w, "Checkpoint", string(agentID))
		return
	}

	s.respondJSON(w, http.StatusOK, checkpoint)
}

// handleRestoreCheckpoint restores an agent from a specific checkpoint version.
//
// @Summary      Restore checkpoint
// @Description  Restores an agent's state from a specific checkpoint version
// @Tags         checkpoints
// @Produce      json
// @Param        id       path  string  true  "Agent ID"
// @Param        version  path  int     true  "Checkpoint version"
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  ProblemDetail
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Failure      500  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/{id}/checkpoints/{version}/restore [post]
func (s *Server) handleRestoreCheckpoint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

	version, err := strconv.Atoi(vars["version"])
	if err != nil {
		s.respondValidationError(w, "Invalid checkpoint version", []FieldError{
			{Field: "version", Message: "must be an integer", Code: "INVALID_VERSION"},
		})
		return
	}
	if version <= 0 {
		s.respondValidationError(w, "Invalid checkpoint version", []FieldError{
			{Field: "version", Message: "must be a positive integer", Code: "INVALID_VERSION"},
		})
		return
	}

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
	if info.Config.TenantID != tenantID {
		s.respondForbidden(w, "Access denied: you do not have permission to access this agent")
		return
	}

	if err := s.checkpointRuntime.RestoreFromCheckpoint(ctx, agentID, version); err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to restore checkpoint: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{
		"status":  "restored",
		"agent":   string(agentID),
		"version": vars["version"],
	})
}

// handleDeleteCheckpoint deletes a specific checkpoint version for an agent.
//
// @Summary      Delete checkpoint
// @Description  Deletes a specific checkpoint version for an agent
// @Tags         checkpoints
// @Produce      json
// @Param        id       path  string  true  "Agent ID"
// @Param        version  path  int     true  "Checkpoint version"
// @Success      204
// @Failure      400  {object}  ProblemDetail
// @Failure      401  {object}  ProblemDetail
// @Failure      403  {object}  ProblemDetail
// @Failure      404  {object}  ProblemDetail
// @Failure      500  {object}  ProblemDetail
// @Security     BearerAuth
// @Router       /agents/{id}/checkpoints/{version} [delete]
func (s *Server) handleDeleteCheckpoint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	agentID := api.AgentID(vars["id"])

	version, err := strconv.Atoi(vars["version"])
	if err != nil {
		s.respondValidationError(w, "Invalid checkpoint version", []FieldError{
			{Field: "version", Message: "must be an integer", Code: "INVALID_VERSION"},
		})
		return
	}
	if version <= 0 {
		s.respondValidationError(w, "Invalid checkpoint version", []FieldError{
			{Field: "version", Message: "must be a positive integer", Code: "INVALID_VERSION"},
		})
		return
	}

	tenantID, err := auth.GetTenantID(ctx)
	if err != nil {
		s.respondUnauthorized(w, "Authentication required: no tenant in context")
		return
	}

	// Verify ownership before allowing deletion.
	info, err := s.runtime.GetAgent(ctx, agentID)
	if err != nil {
		s.respondNotFound(w, "Agent", string(agentID))
		return
	}
	if info.Config.TenantID != tenantID {
		s.respondForbidden(w, "Access denied: you do not have permission to access this agent")
		return
	}

	if err := s.checkpointRuntime.DeleteCheckpoint(ctx, agentID, version); err != nil {
		s.respondInternalError(w, fmt.Sprintf("Failed to delete checkpoint: %v", err))
		return
	}

	s.respondJSON(w, http.StatusNoContent, nil)
}
