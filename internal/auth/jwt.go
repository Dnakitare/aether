// Package auth provides authentication and authorization.
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/dnakitare/aether/pkg/api"
)

// Claims represents JWT claims.
type Claims struct {
	TenantID api.TenantID `json:"tenant_id"`
	UserID   string       `json:"user_id"`
	Role     string       `json:"role"`
	jwt.RegisteredClaims
}

// JWTManager handles JWT creation and validation.
type JWTManager struct {
	secretKey     []byte
	tokenDuration time.Duration
	issuer        string
}

// Config holds JWT manager configuration.
type Config struct {
	SecretKey     string
	TokenDuration time.Duration
	Issuer        string
}

// NewJWTManager creates a new JWT manager.
func NewJWTManager(config Config) (*JWTManager, error) {
	if config.SecretKey == "" {
		return nil, fmt.Errorf("secret key is required")
	}

	if config.TokenDuration == 0 {
		config.TokenDuration = 24 * time.Hour
	}

	if config.Issuer == "" {
		config.Issuer = "aether"
	}

	return &JWTManager{
		secretKey:     []byte(config.SecretKey),
		tokenDuration: config.TokenDuration,
		issuer:        config.Issuer,
	}, nil
}

// GenerateToken generates a JWT token for a user.
func (m *JWTManager) GenerateToken(tenantID api.TenantID, userID, role string) (string, error) {
	now := time.Now()

	claims := Claims{
		TenantID: tenantID,
		UserID:   userID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.tokenDuration)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    m.issuer,
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(m.secretKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// ValidateToken validates a JWT token and returns the claims.
func (m *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.secretKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}

// contextKey is a private type for context keys.
type contextKey string

const (
	// claimsContextKey is the context key for claims.
	claimsContextKey contextKey = "claims"
)

// WithClaims adds claims to a context.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

// GetClaims retrieves claims from a context.
func GetClaims(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*Claims)
	return claims, ok
}

// GetTenantID extracts tenant ID from context claims.
func GetTenantID(ctx context.Context) (api.TenantID, error) {
	claims, ok := GetClaims(ctx)
	if !ok {
		return "", fmt.Errorf("no claims in context")
	}
	return claims.TenantID, nil
}

// GetUserID extracts user ID from context claims.
func GetUserID(ctx context.Context) (string, error) {
	claims, ok := GetClaims(ctx)
	if !ok {
		return "", fmt.Errorf("no claims in context")
	}
	return claims.UserID, nil
}
