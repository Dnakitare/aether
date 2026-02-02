// Package api provides the HTTP API server.
package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aether-runtime/aether/internal/auth"
)

// loggingMiddleware logs HTTP requests.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		s.logger.InfoContext(r.Context(),
			"http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", duration.Milliseconds(),
			"remote_addr", r.RemoteAddr,
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

// corsMiddleware adds CORS headers.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
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

// rateLimitMiddleware enforces rate limits (placeholder for Phase 5).
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: Implement rate limiting in Phase 5
		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
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
