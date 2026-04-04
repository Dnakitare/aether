package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// TenantTierProvider is an interface for looking up tenant tiers.
// Implementations can query databases, caches, or use static configuration.
type TenantTierProvider interface {
	GetTenantTier(ctx context.Context, tenantID api.TenantID) (Tier, error)
}

// StaticTierProvider returns a fixed tier for all tenants.
type StaticTierProvider struct {
	tier Tier
}

// NewStaticTierProvider creates a provider that returns the same tier for all tenants.
func NewStaticTierProvider(tier Tier) *StaticTierProvider {
	return &StaticTierProvider{tier: tier}
}

// GetTenantTier returns the static tier.
func (p *StaticTierProvider) GetTenantTier(ctx context.Context, tenantID api.TenantID) (Tier, error) {
	return p.tier, nil
}

// MiddlewareConfig holds configuration for rate limit middleware.
type MiddlewareConfig struct {
	Logger       *slog.Logger
	Limiter      *MultiLayerLimiter
	TierProvider TenantTierProvider // Optional: If nil, uses default free tier
}

// Middleware creates an HTTP middleware for rate limiting.
func Middleware(logger *slog.Logger, limiter *MultiLayerLimiter) func(http.Handler) http.Handler {
	return MiddlewareWithConfig(MiddlewareConfig{
		Logger:       logger,
		Limiter:      limiter,
		TierProvider: NewStaticTierProvider(TierFree),
	})
}

// MiddlewareWithConfig creates an HTTP middleware with custom configuration.
func MiddlewareWithConfig(config MiddlewareConfig) func(http.Handler) http.Handler {
	if config.TierProvider == nil {
		config.TierProvider = NewStaticTierProvider(TierFree)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract tenant ID from context or header.
			// Fall back to the client IP so unauthenticated endpoints
			// (e.g. POST /v1/auth/token) are still rate-limited.
			tenantID := extractTenantID(r)
			if tenantID == "" {
				tenantID = extractClientIP(r)
			}

			// Extract user ID from context or header
			userID := extractUserID(r)

			// Get tenant tier from provider
			tier, err := config.TierProvider.GetTenantTier(ctx, tenantID)
			if err != nil {
				config.Logger.WarnContext(ctx, "failed to get tenant tier, using free tier",
					"tenant_id", tenantID, "error", err)
				tier = TierFree
			}

			// Check rate limits
			result, err := config.Limiter.CheckLimits(ctx, tenantID, userID, r.URL.Path, tier)
			if err != nil {
				config.Logger.ErrorContext(ctx, "rate limit check failed", "error", err)
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

// extractClientIP returns the client IP address as a synthetic tenant ID for
// unauthenticated requests, enabling IP-based rate limiting.
func extractClientIP(r *http.Request) api.TenantID {
	// Prefer X-Forwarded-For set by a trusted proxy.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// The header may contain a comma-separated list; take the first entry.
		ip := strings.SplitN(xff, ",", 2)[0]
		ip = strings.TrimSpace(ip)
		if ip != "" {
			return api.TenantID("ip:" + ip)
		}
	}
	// Fall back to the direct remote address.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return api.TenantID("ip:" + r.RemoteAddr)
	}
	return api.TenantID("ip:" + host)
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

// Example database-backed tier provider implementation:
//
// type DatabaseTierProvider struct {
//     db *sql.DB
//     cache *cache.Cache // Optional: cache to reduce database load
// }
//
// func (p *DatabaseTierProvider) GetTenantTier(ctx context.Context, tenantID api.TenantID) (Tier, error) {
//     // Check cache first
//     if cached, found := p.cache.Get(string(tenantID)); found {
//         return cached.(Tier), nil
//     }
//
//     // Query database
//     var tierStr string
//     err := p.db.QueryRowContext(ctx,
//         "SELECT tier FROM tenants WHERE id = $1", tenantID).Scan(&tierStr)
//     if err != nil {
//         return TierFree, fmt.Errorf("failed to query tenant tier: %w", err)
//     }
//
//     tier := parseTier(tierStr)
//     p.cache.Set(string(tenantID), tier, 5*time.Minute)
//     return tier, nil
// }

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
