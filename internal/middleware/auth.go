package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/rehmatworks/fastcp/internal/auth"
)

type contextKey string

const (
	UserContextKey         contextKey = "user"
	ClaimsContextKey       contextKey = "claims"
	ImpersonatingContextKey contextKey = "impersonating"
)

// AuthMiddleware validates JWT tokens and sets user context
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, `{"error": "missing authorization header"}`, http.StatusUnauthorized)
			return
		}

		// Extract Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			http.Error(w, `{"error": "invalid authorization header format"}`, http.StatusUnauthorized)
			return
		}

		tokenString := parts[1]

		// Validate token
		claims, err := auth.ValidateToken(tokenString)
		if err != nil {
			if err == auth.ErrTokenExpired {
				http.Error(w, `{"error": "token expired"}`, http.StatusUnauthorized)
				return
			}
			http.Error(w, `{"error": "invalid token"}`, http.StatusUnauthorized)
			return
		}

		ctx := r.Context()

		// Handle impersonation - only admins can impersonate
		impersonateUser := r.Header.Get("X-Impersonate-User")
		if impersonateUser != "" && claims.Role == "admin" {
			// Create modified claims for impersonated user
			impersonatedClaims := &auth.Claims{
				UserID:   impersonateUser,
				Username: impersonateUser,
				Role:     "user", // Impersonated users are always non-admin
			}
			ctx = context.WithValue(ctx, ClaimsContextKey, impersonatedClaims)
			ctx = context.WithValue(ctx, ImpersonatingContextKey, claims) // Store real admin claims
		} else {
			ctx = context.WithValue(ctx, ClaimsContextKey, claims)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// APIKeyMiddleware validates API keys for external integrations
func APIKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for API key in header
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			// Also check query parameter (for webhooks)
			apiKey = r.URL.Query().Get("api_key")
		}

		if apiKey == "" {
			http.Error(w, `{"error": "missing API key"}`, http.StatusUnauthorized)
			return
		}

		// TODO: Validate API key from storage
		// For now, we'll just check if it starts with "fcp_"
		if !strings.HasPrefix(apiKey, "fcp_") {
			http.Error(w, `{"error": "invalid API key"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// AdminOnlyMiddleware ensures only admin users can access
func AdminOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ClaimsContextKey).(*auth.Claims)
		if !ok || claims.Role != "admin" {
			http.Error(w, `{"error": "admin access required"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetClaims retrieves claims from context
func GetClaims(r *http.Request) *auth.Claims {
	claims, _ := r.Context().Value(ClaimsContextKey).(*auth.Claims)
	return claims
}

// GetRealClaims retrieves the real admin claims when impersonating
func GetRealClaims(r *http.Request) *auth.Claims {
	claims, ok := r.Context().Value(ImpersonatingContextKey).(*auth.Claims)
	if ok {
		return claims
	}
	// Not impersonating, return normal claims
	return GetClaims(r)
}

// IsImpersonating returns true if the current request is impersonated
func IsImpersonating(r *http.Request) bool {
	_, ok := r.Context().Value(ImpersonatingContextKey).(*auth.Claims)
	return ok
}

