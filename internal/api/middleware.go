// Package api provides the HTTP API server.
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"

	"github.com/dnakitare/aether/internal/auth"
	"github.com/dnakitare/aether/internal/observability"
	"github.com/dnakitare/aether/pkg/api"
)

// requestIDMiddleware injects a unique X-Request-ID into every request and
// echoes it back in the response, enabling end-to-end correlation.
func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// loggingMiddleware logs HTTP requests and records metrics.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Increment in-flight requests metric
		if s.metrics != nil {
			s.metrics.IncAPIRequestsInFlight(r.Method, r.URL.Path)
			defer s.metrics.DecAPIRequestsInFlight(r.Method, r.URL.Path)
		}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		// Extract tenant ID from context if available
		tenantID := api.TenantID("unknown")
		if claims, ok := auth.GetClaims(r.Context()); ok {
			tenantID = claims.TenantID
		}

		requestID, _ := r.Context().Value(requestIDKey{}).(string)

		// Record metrics
		if s.metrics != nil {
			s.metrics.RecordAPIRequest(
				r.Method,
				r.URL.Path,
				http.StatusText(wrapped.statusCode),
				tenantID,
				duration,
			)
		}

		s.logger.InfoContext(r.Context(),
			"http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", duration.Milliseconds(),
			"remote_addr", r.RemoteAddr,
			"tenant_id", tenantID,
			"request_id", requestID,
		)
	})
}

// recoveryMiddleware recovers from panics.
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				s.logger.ErrorContext(r.Context(),
					"panic recovered",
					"error", err,
					"path", r.URL.Path,
				)

				s.respondError(w, http.StatusInternalServerError, "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers based on the configured allowed origins.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if s.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isAllowedOrigin reports whether origin is in the server's allowed origins list.
func (s *Server) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range s.config.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

// authMiddleware validates JWT authentication on all endpoints except health/metrics.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication for health and metrics endpoints
		if r.URL.Path == "/health" || r.URL.Path == "/readiness" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		// Extract Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			s.logger.WarnContext(r.Context(), "missing authorization header",
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
			)
			s.respondError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		// Extract Bearer token
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			s.logger.WarnContext(r.Context(), "invalid authorization format",
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
			)
			s.respondError(w, http.StatusUnauthorized, "invalid authorization format, expected 'Bearer <token>'")
			return
		}

		// Validate token
		claims, err := s.jwtManager.ValidateToken(tokenString)
		if err != nil {
			s.logger.WarnContext(r.Context(), "invalid token",
				"error", err,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
			)
			s.respondError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		// Add claims to context for handlers
		ctx := auth.WithClaims(r.Context(), claims)

		s.logger.DebugContext(ctx, "authenticated request",
			"tenant_id", claims.TenantID,
			"user_id", claims.UserID,
			"role", claims.Role,
			"path", r.URL.Path,
		)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// tracingMiddleware adds distributed tracing to HTTP requests.
func (s *Server) tracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip if tracing not enabled
		if s.tracer == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Extract trace context from incoming request headers
		ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		// Start a new span for this request
		tracer := s.tracer.Tracer("aether.http")
		ctx, span := observability.StartSpan(
			ctx,
			tracer,
			r.Method+" "+r.URL.Path,
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.String()),
			attribute.String("http.target", r.URL.Path),
			attribute.String("http.scheme", r.URL.Scheme),
			attribute.String("http.host", r.Host),
			attribute.String("http.user_agent", r.UserAgent()),
			attribute.String("http.remote_addr", r.RemoteAddr),
		)
		defer span.End()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Process request with traced context
		next.ServeHTTP(wrapped, r.WithContext(ctx))

		// Add response attributes to span
		span.SetAttributes(
			attribute.Int("http.status_code", wrapped.statusCode),
		)

		// Record error if status code indicates failure
		if wrapped.statusCode >= 400 {
			span.SetAttributes(attribute.Bool("error", true))
		}
	})
}

// requestIDKey is the context key for request IDs.
type requestIDKey struct{}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// requirePermission returns a handler that checks the caller has the given
// permission before delegating to next. When auth is disabled (no claims in
// context) the check is skipped so development mode still works.
func (s *Server) requirePermission(perm auth.Permission, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.GetClaims(r.Context())
		if ok {
			if err := auth.CheckPermission(claims, perm); err != nil {
				s.respondError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
		}
		next(w, r)
	}
}

// Standard error responses

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

// NewErrorResponse creates an error response.
func NewErrorResponse(message string) *ErrorResponse {
	return &ErrorResponse{Error: message}
}

// WithCode adds an error code.
func (e *ErrorResponse) WithCode(code string) *ErrorResponse {
	e.Code = code
	return e
}

// WithDetails adds error details.
func (e *ErrorResponse) WithDetails(details string) *ErrorResponse {
	e.Details = details
	return e
}

// String implements fmt.Stringer.
func (e *ErrorResponse) String() string {
	if e.Code != "" {
		return fmt.Sprintf("[%s] %s", e.Code, e.Error)
	}
	return e.Error
}
