package ratelimit

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// Middleware creates an HTTP middleware for rate limiting.
func Middleware(logger *slog.Logger, limiter *MultiLayerLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract tenant ID from context or header
			tenantID := extractTenantID(r)
			if tenantID == "" {
				// No tenant ID, skip rate limiting
				next.ServeHTTP(w, r)
				return
			}

			// Extract user ID from context or header
			userID := extractUserID(r)

			// Get tenant tier (default to free if not found)
			tier := getTenantTier(tenantID)

			// Check rate limits
			result, err := limiter.CheckLimits(ctx, tenantID, userID, r.URL.Path, tier)
			if err != nil {
				logger.ErrorContext(ctx, "rate limit check failed", "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))

			// Check if allowed
			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())))
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			// Request is allowed
			next.ServeHTTP(w, r)
		})
	}
}

// extractTenantID extracts tenant ID from request.
func extractTenantID(r *http.Request) api.TenantID {
	// Try header first
	if tenantID := r.Header.Get("X-Tenant-ID"); tenantID != "" {
		return api.TenantID(tenantID)
	}

	// Try from context (set by auth middleware)
	if tenantID := r.Context().Value("tenant_id"); tenantID != nil {
		if tid, ok := tenantID.(api.TenantID); ok {
			return tid
		}
		if tid, ok := tenantID.(string); ok {
			return api.TenantID(tid)
		}
	}

	return ""
}

// extractUserID extracts user ID from request.
func extractUserID(r *http.Request) string {
	// Try header first
	if userID := r.Header.Get("X-User-ID"); userID != "" {
		return userID
	}

	// Try from context (set by auth middleware)
	if userID := r.Context().Value("user_id"); userID != nil {
		if uid, ok := userID.(string); ok {
			return uid
		}
	}

	return ""
}

// getTenantTier returns the tier for a tenant.
// In production, this would query the database.
// For now, we return a default tier.
func getTenantTier(tenantID api.TenantID) Tier {
	// TODO: Look up tenant tier from database
	// For now, default to free tier
	return TierFree
}

// RateLimitInfo holds rate limit information for responses.
type RateLimitInfo struct {
	Limit      int       `json:"limit"`
	Remaining  int       `json:"remaining"`
	Reset      time.Time `json:"reset"`
	RetryAfter int       `json:"retry_after,omitempty"`
}

// WriteRateLimitHeaders writes rate limit headers to the response.
func WriteRateLimitHeaders(w http.ResponseWriter, result *Result) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))

	if !result.Allowed && result.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())))
	}
}

// TooManyRequestsResponse writes a 429 response with rate limit info.
func TooManyRequestsResponse(w http.ResponseWriter, result *Result) {
	WriteRateLimitHeaders(w, result)
	w.WriteHeader(http.StatusTooManyRequests)
	fmt.Fprintf(w, `{"error":"Rate limit exceeded","retry_after":%d}`, int(result.RetryAfter.Seconds()))
}
