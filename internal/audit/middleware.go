// Package audit provides immutable audit logging.
package audit

import (
	"context"
	"net/http"

	"github.com/dnakitare/aether/internal/auth"
)

// Middleware creates HTTP middleware for audit logging.
func Middleware(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract claims from context (if authenticated)
			claims, _ := auth.GetClaims(ctx)

			// Capture response
			wrapped := &auditResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Call next handler
			next.ServeHTTP(wrapped, r)

			// Log audit event (async to avoid blocking)
			go func() {
				event := &Event{
					Action:    mapHTTPMethodToAction(r.Method),
					Resource:  extractResourceFromPath(r.URL.Path),
					IPAddress: r.RemoteAddr,
					UserAgent: r.UserAgent(),
				}

				if claims != nil {
					event.TenantID = claims.TenantID
					event.UserID = claims.UserID
				}

				if wrapped.statusCode >= 200 && wrapped.statusCode < 300 {
					event.Result = ResultSuccess
				} else if wrapped.statusCode == http.StatusForbidden {
					event.Result = ResultDenied
				} else {
					event.Result = ResultFailure
				}

				_ = logger.Log(context.Background(), event)
			}()
		})
	}
}

// auditResponseWriter wraps http.ResponseWriter to capture status code.
type auditResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *auditResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// mapHTTPMethodToAction maps HTTP methods to audit actions.
func mapHTTPMethodToAction(method string) Action {
	switch method {
	case http.MethodPost:
		return ActionCreate
	case http.MethodGet:
		return ActionRead
	case http.MethodPut, http.MethodPatch:
		return ActionUpdate
	case http.MethodDelete:
		return ActionDelete
	default:
		return ActionRead
	}
}

// extractResourceFromPath extracts resource type from URL path.
func extractResourceFromPath(path string) ResourceType {
	// Simple extraction - in production, use proper routing
	if len(path) > 4 && path[:4] == "/v1/" {
		path = path[4:]
	}

	if len(path) > 0 {
		for i, ch := range path {
			if ch == '/' {
				return ResourceType(path[:i])
			}
		}
		return ResourceType(path)
	}

	return ""
}
