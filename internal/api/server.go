// Package api provides the HTTP API server.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/internal/scaler"
	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/internal/tenant"
	"github.com/aether-runtime/aether/pkg/api"
)

// Server is the HTTP API server.
type Server struct {
	logger *slog.Logger
	router *mux.Router
	server *http.Server

	// Dependencies
	runtime      api.Runtime
	scheduler    *scheduler.Scheduler
	scaler       *scaler.Scaler
	quotaManager *tenant.QuotaManager
	jwtManager   *auth.JWTManager

	// Configuration
	config Config
}

// Config holds server configuration.
type Config struct {
	// Address to listen on (e.g., ":8080")
	Address string

	// ReadTimeout for requests
	ReadTimeout time.Duration

	// WriteTimeout for responses
	WriteTimeout time.Duration

	// EnableCORS enables CORS headers
	EnableCORS bool

	// EnableAuth enables JWT authentication (should be true in production)
	EnableAuth bool
}

// New creates a new HTTP API server.
func New(logger *slog.Logger, config Config, runtime api.Runtime, sched *scheduler.Scheduler, sc *scaler.Scaler, qm *tenant.QuotaManager, jwtMgr *auth.JWTManager) *Server {
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 15 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 15 * time.Second
	}

	s := &Server{
		logger:       logger.With("component", "api_server"),
		router:       mux.NewRouter(),
		runtime:      runtime,
		scheduler:    sched,
		scaler:       sc,
		quotaManager: qm,
		jwtManager:   jwtMgr,
		config:       config,
	}

	s.setupRoutes()

	s.server = &http.Server{
		Addr:         config.Address,
		Handler:      s.router,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	}

	return s
}

// Start starts the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	s.logger.InfoContext(ctx, "starting HTTP API server", "address", s.config.Address)

	errCh := make(chan error, 1)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server failed to start: %w", err)
	case <-time.After(100 * time.Millisecond):
		s.logger.InfoContext(ctx, "HTTP API server started successfully")
		return nil
	}
}

// Stop gracefully stops the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	s.logger.InfoContext(ctx, "stopping HTTP API server")

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	s.logger.InfoContext(ctx, "HTTP API server stopped")
	return nil
}

// setupRoutes configures all API routes.
func (s *Server) setupRoutes() {
	// Global middleware (applied to all routes)
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.recoveryMiddleware)
	if s.config.EnableCORS {
		s.router.Use(s.corsMiddleware)
	}

	// Health checks (unauthenticated)
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")
	s.router.HandleFunc("/readiness", s.handleReadiness).Methods("GET")

	// API v1 (authenticated)
	v1 := s.router.PathPrefix("/v1").Subrouter()

	// Apply authentication middleware to all v1 routes if enabled
	if s.config.EnableAuth {
		v1.Use(s.authMiddleware)
	}

	// Agents
	v1.HandleFunc("/agents", s.handleListAgents).Methods("GET")
	v1.HandleFunc("/agents", s.handleCreateAgent).Methods("POST")
	v1.HandleFunc("/agents/{id}", s.handleGetAgent).Methods("GET")
	v1.HandleFunc("/agents/{id}", s.handleDeleteAgent).Methods("DELETE")
	v1.HandleFunc("/agents/{id}/logs", s.handleGetAgentLogs).Methods("GET")
	v1.HandleFunc("/agents/{id}/health", s.handleGetAgentHealth).Methods("GET")

	// Quotas
	v1.HandleFunc("/quotas", s.handleListQuotas).Methods("GET")
	v1.HandleFunc("/quotas/{tenant_id}", s.handleGetQuota).Methods("GET")
	v1.HandleFunc("/quotas/{tenant_id}", s.handleSetQuota).Methods("PUT")
	v1.HandleFunc("/quotas/{tenant_id}/usage", s.handleGetUsage).Methods("GET")

	// Scheduler
	v1.HandleFunc("/scheduler/stats", s.handleGetSchedulerStats).Methods("GET")
	v1.HandleFunc("/scheduler/nodes", s.handleListNodes).Methods("GET")

	// Scaler
	v1.HandleFunc("/scaler/policies", s.handleListPolicies).Methods("GET")
	v1.HandleFunc("/scaler/policies", s.handleCreatePolicy).Methods("POST")
	v1.HandleFunc("/scaler/policies/{name}", s.handleGetPolicy).Methods("GET")
	v1.HandleFunc("/scaler/policies/{name}", s.handleDeletePolicy).Methods("DELETE")
}

// respondJSON sends a JSON response.
func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			s.logger.Error("failed to encode JSON response", "error", err)
		}
	}
}

// respondError sends an error response.
func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	s.respondJSON(w, status, map[string]string{
		"error": message,
	})
}

// parseJSON parses JSON from request body.
func (s *Server) parseJSON(r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
