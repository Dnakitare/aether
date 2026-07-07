package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/lib/pq"

	"github.com/dnakitare/aether/internal/auth"
	"github.com/dnakitare/aether/pkg/api"
)

// TokenIssuer validates an API key and returns a signed JWT.
type TokenIssuer interface {
	IssueToken(ctx context.Context, apiKey string) (string, error)
}

// IssueTokenRequest is the body for POST /v1/auth/token.
type IssueTokenRequest struct {
	APIKey string `json:"api_key"`
}

// IssueTokenResponse carries the issued JWT.
type IssueTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // seconds
}

// handleIssueToken exchanges a valid API key for a signed JWT.
//
// @Summary      Issue token
// @Description  Exchanges a valid API key for a short-lived JWT bearer token
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      IssueTokenRequest   true  "API key"
// @Success      200      {object}  IssueTokenResponse
// @Failure      400      {object}  ProblemDetail
// @Failure      401      {object}  ProblemDetail
// @Router       /v1/auth/token [post]
func (s *Server) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.tokenIssuer == nil {
		s.respondError(w, http.StatusServiceUnavailable, "token issuance not configured")
		return
	}

	var req IssueTokenRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.respondValidationError(w, "Invalid request body", []FieldError{
			{Field: "body", Message: err.Error(), Code: "INVALID_JSON"},
		})
		return
	}

	if req.APIKey == "" {
		s.respondValidationError(w, "Missing API key", []FieldError{
			{Field: "api_key", Message: "api_key is required", Code: "REQUIRED"},
		})
		return
	}

	token, err := s.tokenIssuer.IssueToken(ctx, req.APIKey)
	if err != nil {
		// Don't expose whether the key exists — always return 401.
		s.logger.WarnContext(ctx, "token issuance failed", "error", err)
		s.respondUnauthorized(w, "Invalid or expired API key")
		return
	}

	s.respondJSON(w, http.StatusOK, IssueTokenResponse{
		Token:     token,
		ExpiresIn: 86400, // 24 h, matches JWTManager default
	})
}

// PostgresTokenIssuer validates API keys against the database and issues JWTs.
type PostgresTokenIssuer struct {
	db         *sql.DB
	logger     *slog.Logger
	jwtManager interface {
		GenerateToken(tenantID api.TenantID, userID, role string) (string, error)
	}
}

// NewPostgresTokenIssuer creates a TokenIssuer backed by the api_keys table.
func NewPostgresTokenIssuer(db *sql.DB, jwtManager interface {
	GenerateToken(tenantID api.TenantID, userID, role string) (string, error)
}, logger *slog.Logger) *PostgresTokenIssuer {
	return &PostgresTokenIssuer{db: db, jwtManager: jwtManager, logger: logger}
}

// IssueToken looks up the API key hash in the database and, if valid, returns a JWT.
func (p *PostgresTokenIssuer) IssueToken(ctx context.Context, apiKey string) (string, error) {
	keyHash := hashAPIKey(apiKey)

	var (
		id        string
		tenantID  string
		scopes    []string
		expiresAt sql.NullTime
		revokedAt sql.NullTime
	)

	err := p.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, scopes, expires_at, revoked_at
		FROM api_keys
		WHERE key_hash = $1
	`, keyHash).Scan(&id, &tenantID, pq.Array(&scopes), &expiresAt, &revokedAt)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("key not found")
	}
	if err != nil {
		return "", fmt.Errorf("database error: %w", err)
	}

	if revokedAt.Valid {
		return "", fmt.Errorf("key has been revoked")
	}
	if expiresAt.Valid && time.Now().After(expiresAt.Time) {
		return "", fmt.Errorf("key has expired")
	}

	// Derive the RBAC role from scopes. Order matters: platform authority is
	// explicit and never implied by tenant-level write scopes. A key with no
	// recognized scope falls back to viewer (read-only) rather than a role with
	// no permissions, so it isn't silently locked out.
	role := string(auth.RoleViewer)
	scopeSet := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		scopeSet[s] = true
	}
	switch {
	case scopeSet["platform:admin"]:
		role = string(auth.RolePlatformAdmin)
	case scopeSet["admin"]:
		role = string(auth.RoleAdmin)
	case scopeSet["agent:create"] || scopeSet["agent:update"] || scopeSet["agent:delete"]:
		role = string(auth.RoleDeveloper)
	}

	// Update last_used_at best-effort; don't fail the request if it errors.
	if _, err := p.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, id); err != nil {
		// Non-fatal: log but don't fail the token issuance
		p.logger.WarnContext(ctx, "failed to update last_used_at", "key_id", id, "error", err)
	}

	return p.jwtManager.GenerateToken(api.TenantID(tenantID), id, role)
}

// hashAPIKey returns the SHA-256 hex digest of an API key, matching the
// format used when keys are stored (see internal/auth/apikey.go hashKey).
func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
