// Package api provides the HTTP API server.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"

	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/internal/observability"
	"github.com/aether-runtime/aether/internal/scaler"
	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/internal/shutdown"
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

	// Graceful shutdown support
	healthChecker  *HealthChecker
	shutdownMgr    *shutdown.Manager
	activeRequests atomic.Int64
	accepting      atomic.Bool
	wg             sync.WaitGroup

	// Observability
	metrics *observability.MetricsCollector
	tracer  *observability.TracerProvider
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

	// TracingConfig for distributed tracing (optional)
	TracingConfig *observability.TracerConfig
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

	// Initialize health checker
	s.healthChecker = NewHealthChecker(logger)

	// Initialize shutdown manager
	s.shutdownMgr = shutdown.NewManager(logger, shutdown.DefaultConfig())

	// Initialize metrics collector
	s.metrics = observability.NewMetricsCollector(logger)

	// Initialize tracing if configured
	if config.TracingConfig != nil {
		tracer, err := observability.NewTracerProvider(logger, *config.TracingConfig)
		if err != nil {
			logger.Error("failed to initialize tracing", "error", err)
		} else {
			s.tracer = tracer
		}
	}

	// Start accepting requests
	s.accepting.Store(true)

	// Register shutdown hooks
	s.registerShutdownHooks()

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

	// Use shutdown manager for graceful shutdown
	if err := s.shutdownMgr.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	s.logger.InfoContext(ctx, "HTTP API server stopped")
	return nil
}

// Router returns the underlying HTTP router for testing purposes.
func (s *Server) Router() http.Handler {
	return s.router
}

// setupRoutes configures all API routes.
func (s *Server) setupRoutes() {
	// Global middleware (applied to all routes)
	s.router.Use(s.requestTrackingMiddleware) // Must be first for graceful shutdown
	s.router.Use(s.tracingMiddleware)         // Add tracing context early
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.recoveryMiddleware)
	if s.config.EnableCORS {
		s.router.Use(s.corsMiddleware)
	}

	// Health checks (unauthenticated) - use HealthChecker
	s.router.HandleFunc("/health", s.healthChecker.Health).Methods("GET")
	s.router.HandleFunc("/readiness", s.healthChecker.Readiness).Methods("GET")

	// Prometheus metrics (unauthenticated)
	s.router.Handle("/metrics", s.metrics.Handler()).Methods("GET")

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

	// Bulk operations
	v1.HandleFunc("/agents/bulk/create", s.handleBulkCreateAgents).Methods("POST")
	v1.HandleFunc("/agents/bulk/delete", s.handleBulkDeleteAgents).Methods("POST")

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

// registerShutdownHooks registers graceful shutdown hooks in priority order.
func (s *Server) registerShutdownHooks() {
	// Priority 10: Stop accepting new requests
	s.shutdownMgr.RegisterHook("stop_accepting_requests", shutdown.PriorityStopAcceptingRequests, func(ctx context.Context) error {
		s.logger.InfoContext(ctx, "stopping acceptance of new requests")
		s.accepting.Store(false)
		return nil
	})

	// Priority 20: Drain in-flight requests
	s.shutdownMgr.RegisterHook("drain_requests", shutdown.PriorityDrainRequests, func(ctx context.Context) error {
		s.logger.InfoContext(ctx, "draining in-flight requests", "active", s.activeRequests.Load())

		// Wait for in-flight requests with timeout
		done := make(chan struct{})
		go func() {
			s.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			s.logger.InfoContext(ctx, "all requests drained")
			return nil
		case <-ctx.Done():
			remaining := s.activeRequests.Load()
			s.logger.WarnContext(ctx, "timeout while draining requests", "remaining", remaining)
			return fmt.Errorf("timeout draining requests, %d remaining", remaining)
		}
	})

	// Priority 40: Close HTTP server
	s.shutdownMgr.RegisterHook("close_http_server", shutdown.PriorityCloseConnections, func(ctx context.Context) error {
		s.logger.InfoContext(ctx, "closing HTTP server")
		if err := s.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("HTTP server shutdown failed: %w", err)
		}
		return nil
	})

	// Priority 50: Shutdown tracer (after HTTP server closed)
	if s.tracer != nil {
		s.shutdownMgr.RegisterHook("shutdown_tracer", shutdown.PriorityCleanup, func(ctx context.Context) error {
			s.logger.InfoContext(ctx, "shutting down tracer")
			if err := s.tracer.Shutdown(ctx); err != nil {
				return fmt.Errorf("tracer shutdown failed: %w", err)
			}
			return nil
		})
	}
}

// requestTrackingMiddleware tracks active requests for graceful shutdown.
func (s *Server) requestTrackingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if accepting new requests
		if !s.accepting.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "server is shutting down",
			})
			return
		}

		// Track this request
		s.activeRequests.Add(1)
		s.wg.Add(1)
		defer func() {
			s.activeRequests.Add(-1)
			s.wg.Done()
		}()

		next.ServeHTTP(w, r)
	})
}

// RegisterHealthCheck registers a health check for a dependency.
// This should be called during initialization to register checks for
// PostgreSQL, Redis, etcd, Kafka, etc.
func (s *Server) RegisterHealthCheck(name string, check HealthCheck) {
	s.healthChecker.RegisterCheck(name, check)
}

// ShutdownManager returns the shutdown manager for registering custom hooks.
func (s *Server) ShutdownManager() *shutdown.Manager {
	return s.shutdownMgr
}

// Metrics returns the metrics collector for recording custom metrics.
func (s *Server) Metrics() *observability.MetricsCollector {
	return s.metrics
}

// Tracer returns the tracer provider for creating spans.
func (s *Server) Tracer() *observability.TracerProvider {
	return s.tracer
}
