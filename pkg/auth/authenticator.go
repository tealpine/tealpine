package auth

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"tealpine/pkg/config"
)

// Authenticator handles HTTP bearer token and OIDC authentication
type Authenticator struct {
	userTokens   map[string]string  // token -> username (for static tokens)
	users        map[string]bool    // username -> exists (for OIDC validation)
	oidcProvider *OIDCProvider      // Optional OIDC provider
	sessionStore *SessionStore      // Optional session store
	authConfig   *config.AuthConfig // OIDC configuration
	enabled      bool               // whether auth is enabled
}

// NewAuthenticator creates a new authenticator with the given users and auth config
// If no users are provided and no OIDC config, authentication is disabled
func NewAuthenticator(users map[string]config.UserConfig, authConfig *config.AuthConfig, redirectURL string) *Authenticator {
	// Check if authentication is disabled
	if len(users) == 0 && (authConfig == nil || authConfig.Type == "") {
		return &Authenticator{
			enabled: false,
		}
	}

	// Build token map for static tokens
	userTokens := make(map[string]string)
	usersMap := make(map[string]bool)
	for username, userConfig := range users {
		userTokens[userConfig.Token] = username
		usersMap[username] = true
	}

	auth := &Authenticator{
		userTokens: userTokens,
		users:      usersMap,
		authConfig: authConfig,
		enabled:    true,
	}

	// Initialize OIDC if configured
	if authConfig != nil && authConfig.Type == "oidc" {
		provider, err := NewOIDCProvider(authConfig, redirectURL)
		if err != nil {
			log.Printf("Failed to initialize OIDC provider: %v. Falling back to static tokens.", err)
		} else {
			auth.oidcProvider = provider
			auth.sessionStore = NewSessionStore()
			log.Println("OIDC authentication enabled")
		}
	}

	return auth
}

// Middleware returns an HTTP middleware that authenticates requests using bearer tokens or sessions
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
	// 1. Try session-based auth first (if OIDC enabled)
	if a.sessionStore != nil {
		if username := a.checkSession(r); username != "" {
			return username, true
		}
	}

	// 2. Extract Bearer token
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

	// 3. Try OIDC token validation if OIDC is enabled
	if a.oidcProvider != nil {
		if looksLikeJWT(token) {
			// Try JWT validation (for ID tokens or JWT access tokens)
			username, err := a.validateJWT(r.Context(), token)
			if err == nil {
				return username, true
			}
			log.Printf("JWT validation failed: %v, trying token introspection", err)
		}

		// Try token introspection (for opaque access tokens)
		username, err := a.validateAccessToken(r.Context(), token)
		if err == nil {
			return username, true
		}
		log.Printf("Token introspection failed: %v, trying static token", err)
	}

	// 4. Fall back to static token lookup
	username, ok := a.userTokens[token]
	if !ok {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return "", false
	}
	return username, true
}

// checkSession checks if there's a valid session cookie
func (a *Authenticator) checkSession(r *http.Request) string {
	sessionID, err := GetSessionCookie(r)
	if err != nil {
		return ""
	}

	session, ok := a.sessionStore.GetSession(sessionID)
	if !ok {
		return ""
	}

	return session.Username
}

// looksLikeJWT checks if a token has the format of a JWT (3 base64url parts separated by dots)
func looksLikeJWT(token string) bool {
	parts := strings.Split(token, ".")
	return len(parts) == 3
}

// validateJWT validates a JWT token and returns the username
func (a *Authenticator) validateJWT(ctx context.Context, token string) (string, error) {
	// Validate JWT using OIDC provider
	idToken, err := a.oidcProvider.ValidateJWT(ctx, token)
	if err != nil {
		return "", err
	}

	// Extract claims
	claims, err := a.oidcProvider.ExtractClaims(idToken)
	if err != nil {
		return "", err
	}

	// Get username from claims
	username, _, err := a.getUserFromJWT(claims)
	if err != nil {
		return "", err
	}

	return username, nil
}

// validateAccessToken validates an opaque access token via token introspection
func (a *Authenticator) validateAccessToken(ctx context.Context, token string) (string, error) {
	// Introspect the token (calls userinfo endpoint)
	claims, err := a.oidcProvider.IntrospectToken(ctx, token)
	if err != nil {
		return "", err
	}

	// Get username from claims
	username, _, err := a.getUserFromJWT(claims)
	if err != nil {
		return "", err
	}

	return username, nil
}

// getUserFromJWT extracts username and groups from JWT claims
func (a *Authenticator) getUserFromJWT(claims map[string]interface{}) (string, []string, error) {
	// Determine which claim field to use for username
	userClaimField := "email"
	if a.authConfig != nil && a.authConfig.UserClaimField != "" {
		userClaimField = a.authConfig.UserClaimField
	}

	// Extract user identifier from claims
	userID, ok := claims[userClaimField].(string)
	if !ok {
		return "", nil, fmt.Errorf("claim '%s' not found or not a string", userClaimField)
	}

	// Default to requiring user in config
	requireUserInConfig := true
	if a.authConfig != nil {
		requireUserInConfig = a.authConfig.RequireUserInConfig
	}

	// Check if user exists in config (if required)
	if requireUserInConfig {
		if !a.users[userID] {
			return "", nil, fmt.Errorf("user '%s' not found in configuration", userID)
		}
		// In strict mode, we don't extract groups from JWT, they come from config
		log.Printf("OIDC user mapped (strict mode): %s=%s", userClaimField, userID)
		return userID, nil, nil
	}

	// Open mode: allow any authenticated user, extract groups from JWT
	groups := a.extractGroupsFromClaims(claims)
	log.Printf("OIDC user mapped (open mode): %s=%s, groups=%v", userClaimField, userID, groups)
	return userID, groups, nil
}

// extractGroupsFromClaims extracts groups from JWT claims
func (a *Authenticator) extractGroupsFromClaims(claims map[string]interface{}) []string {
	// Determine which claim field to use for groups
	groupClaimField := "groups"
	if a.authConfig != nil && a.authConfig.GroupClaimField != "" {
		groupClaimField = a.authConfig.GroupClaimField
	}

	// Try to extract groups claim
	groupsClaim, ok := claims[groupClaimField]
	if !ok {
		return nil
	}

	// Handle string array format
	if groupsArray, ok := groupsClaim.([]interface{}); ok {
		groups := make([]string, 0, len(groupsArray))
		for _, g := range groupsArray {
			if groupStr, ok := g.(string); ok && isValidGroupName(groupStr) {
				groups = append(groups, groupStr)
			}
		}
		return groups
	}

	// Handle comma-separated string format
	if groupsStr, ok := groupsClaim.(string); ok {
		parts := strings.Split(groupsStr, ",")
		groups := make([]string, 0, len(parts))
		for _, part := range parts {
			group := strings.TrimSpace(part)
			if isValidGroupName(group) {
				groups = append(groups, group)
			}
		}
		return groups
	}

	return nil
}

// isValidGroupName validates a group name
func isValidGroupName(name string) bool {
	// Allow alphanumeric, underscore, and hyphen
	match, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, name)
	return match
}

// GetOIDCProvider returns the OIDC provider if configured
func (a *Authenticator) GetOIDCProvider() *OIDCProvider {
	return a.oidcProvider
}

// GetSessionStore returns the session store if configured
func (a *Authenticator) GetSessionStore() *SessionStore {
	return a.sessionStore
}

// MapUserFromClaims maps JWT claims to internal username (exposed for OAuth handlers)
func (a *Authenticator) MapUserFromClaims(claims map[string]interface{}) (string, error) {
	username, _, err := a.getUserFromJWT(claims)
	return username, err
}
