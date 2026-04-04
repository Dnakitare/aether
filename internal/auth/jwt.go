// Package auth provides authentication and authorization.
package auth

import (
	"context"
	"crypto/rsa"
	"fmt"
	"os"
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
// It supports HS256 (symmetric) when only SecretKey is provided, and RS256
// (asymmetric) when PrivateKeyPEM / PublicKeyPEM are provided.
type JWTManager struct {
	secretKey     []byte
	privateKey    *rsa.PrivateKey
	publicKey     *rsa.PublicKey
	tokenDuration time.Duration
	issuer        string
}

// Config holds JWT manager configuration.
type Config struct {
	// SecretKey is used for HS256 signing. Required when PrivateKeyPath is empty.
	SecretKey string

	// PrivateKeyPath and PublicKeyPath enable RS256. When both are set, RS256 is
	// preferred and SecretKey is ignored.
	PrivateKeyPath string
	PublicKeyPath  string

	TokenDuration time.Duration
	Issuer        string
}

// NewJWTManager creates a new JWT manager from cfg.
// RS256 is used when both PrivateKeyPath and PublicKeyPath are non-empty;
// otherwise HS256 is used and SecretKey must be set.
func NewJWTManager(config Config) (*JWTManager, error) {
	if config.TokenDuration == 0 {
		config.TokenDuration = 24 * time.Hour
	}
	if config.Issuer == "" {
		config.Issuer = "aether"
	}

	m := &JWTManager{
		tokenDuration: config.TokenDuration,
		issuer:        config.Issuer,
	}

	if config.PrivateKeyPath != "" && config.PublicKeyPath != "" {
		privPEM, err := os.ReadFile(config.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read JWT private key: %w", err)
		}
		pubPEM, err := os.ReadFile(config.PublicKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read JWT public key: %w", err)
		}

		privKey, err := jwt.ParseRSAPrivateKeyFromPEM(privPEM)
		if err != nil {
			return nil, fmt.Errorf("failed to parse JWT private key: %w", err)
		}
		pubKey, err := jwt.ParseRSAPublicKeyFromPEM(pubPEM)
		if err != nil {
			return nil, fmt.Errorf("failed to parse JWT public key: %w", err)
		}

		m.privateKey = privKey
		m.publicKey = pubKey
		return m, nil
	}

	// Fall back to HS256.
	if config.SecretKey == "" {
		return nil, fmt.Errorf("secret key is required when RSA key paths are not set")
	}
	m.secretKey = []byte(config.SecretKey)
	return m, nil
}

// isRS256 reports whether this manager uses asymmetric RS256 signing.
func (m *JWTManager) isRS256() bool {
	return m.privateKey != nil && m.publicKey != nil
}

// GenerateToken generates a signed JWT for the given user.
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

	var (
		token *jwt.Token
		key   interface{}
	)

	if m.isRS256() {
		token = jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		key = m.privateKey
	} else {
		token = jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		key = m.secretKey
	}

	tokenString, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// ValidateToken validates a JWT and returns its claims.
// Accepts both HS256 and RS256 tokens depending on how the manager was created.
func (m *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if m.isRS256() {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return m.publicKey, nil
		}
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
