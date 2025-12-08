package auth

import (
	"context"
	"net/http"
	"strings"
)

// Authenticator handles HTTP bearer token authentication
type Authenticator struct {
	userTokens map[string]string // token -> username
	enabled    bool              // whether auth is enabled
}

// NewAuthenticator creates a new authenticator with the given users
// If no users are provided, authentication is disabled
func NewAuthenticator(users map[string]*UserInfo) *Authenticator {
	if len(users) == 0 {
		return &Authenticator{
			enabled: false,
		}
	}

	// Build token map
	userTokens := make(map[string]string)
	for username, userInfo := range users {
		userTokens[userInfo.Token] = username
	}

	return &Authenticator{
		userTokens: userTokens,
		enabled:    true,
	}
}

// Middleware returns an HTTP middleware that authenticates requests using bearer tokens
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If auth is disabled, pass through
		if !a.enabled {
			next.ServeHTTP(w, r)
			return
		}

		username, ok := a.authenticateUser(w, r)
		if !ok {
			return
		}

		// Store username in context for downstream handlers
		ctx := context.WithValue(r.Context(), CtxUsernameKey, username)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

func (a *Authenticator) authenticateUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	// Extract bearer token from the Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "missing authorization header", http.StatusUnauthorized)
		return "", false
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
		return "", false
	}

	// Find user by token
	username, ok := a.userTokens[token]
	if !ok {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return "", false
	}
	return username, true
}
