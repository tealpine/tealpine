package server

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"tealpine/pkg/auth"
)

// OAuthHandlers holds OAuth-related HTTP handlers
type OAuthHandlers struct {
	authenticator    *auth.Authenticator
	cookieSecure     bool
}

// NewOAuthHandlers creates a new OAuthHandlers instance
func NewOAuthHandlers(authenticator *auth.Authenticator, cookieSecure bool) *OAuthHandlers {
	return &OAuthHandlers{
		authenticator: authenticator,
		cookieSecure:  cookieSecure,
	}
}

// HandleLogin initiates the OAuth authorization code flow
func (h *OAuthHandlers) HandleLogin(c *gin.Context) {
	// Get OIDC provider and session store from authenticator
	provider, sessionStore := h.getOIDCComponents()
	if provider == nil || sessionStore == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "OIDC not configured"})
		return
	}

	// Get or create session
	sessionID, err := auth.GetSessionCookie(c.Request)
	if err != nil || sessionID == "" {
		// Create new anonymous session for OAuth flow
		sessionID, err = sessionStore.CreateSession("")
		if err != nil {
			log.Printf("Failed to create session: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
			return
		}
		auth.SetSessionCookie(c.Writer, sessionID, h.cookieSecure)
	}

	// Generate PKCE code verifier (43-128 random bytes, base64url)
	codeVerifier, err := auth.GeneratePKCEVerifier()
	if err != nil {
		log.Printf("Failed to generate PKCE verifier: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PKCE verifier"})
		return
	}

	// Generate CSRF state token (32 random bytes, hex)
	state, err := auth.GenerateStateToken()
	if err != nil {
		log.Printf("Failed to generate state token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate state token"})
		return
	}

	// Store state and verifier in session
	if err := sessionStore.StoreOAuthState(sessionID, state, codeVerifier); err != nil {
		log.Printf("Failed to store OAuth state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store OAuth state"})
		return
	}

	// Build authorization URL with OIDC provider
	// Includes: client_id, redirect_uri, scope, state, code_challenge, code_challenge_method=S256
	authURL := provider.GenerateAuthURL(state, codeVerifier)

	// Redirect user to authorization URL
	c.Redirect(http.StatusFound, authURL)
}

// HandleCallback handles the OAuth callback after user authorization
func (h *OAuthHandlers) HandleCallback(c *gin.Context) {
	// Get OIDC provider and session store from authenticator
	provider, sessionStore := h.getOIDCComponents()
	if provider == nil || sessionStore == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "OIDC not configured"})
		return
	}

	// Extract code and state from query parameters
	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "authorization code required"})
		return
	}

	state := c.Query("state")
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state parameter required"})
		return
	}

	// Get session from cookie
	sessionID, err := auth.GetSessionCookie(c.Request)
	if err != nil || sessionID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	// Validate CSRF state token matches session
	storedState, codeVerifier, err := sessionStore.GetOAuthState(sessionID)
	if err != nil {
		log.Printf("Failed to get OAuth state: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session"})
		return
	}

	if storedState != state {
		log.Printf("CSRF validation failed: state mismatch")
		c.JSON(http.StatusForbidden, gin.H{"error": "CSRF validation failed"})
		return
	}

	// Exchange authorization code for tokens with OIDC provider
	token, err := provider.ExchangeCode(c.Request.Context(), code, codeVerifier)
	if err != nil {
		log.Printf("Failed to exchange authorization code: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
		return
	}

	// Extract ID token from OAuth2 token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		log.Printf("No ID token in OAuth2 response")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
		return
	}

	// Validate ID token
	idToken, err := provider.ValidateJWT(c.Request.Context(), rawIDToken)
	if err != nil {
		log.Printf("Failed to validate ID token: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
		return
	}

	// Extract user claims
	claims, err := provider.ExtractClaims(idToken)
	if err != nil {
		log.Printf("Failed to extract claims: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication failed"})
		return
	}

	// Map user to internal username
	username, err := h.mapUserFromClaims(claims)
	if err != nil {
		log.Printf("Failed to map user: %v", err)
		c.JSON(http.StatusForbidden, gin.H{"error": "user not authorized"})
		return
	}

	// Delete old session and create new authenticated session
	sessionStore.DeleteSession(sessionID)
	newSessionID, err := sessionStore.CreateSession(username)
	if err != nil {
		log.Printf("Failed to create authenticated session: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}

	// Set new session cookie
	auth.SetSessionCookie(c.Writer, newSessionID, h.cookieSecure)

	// Redirect to home page or original destination
	c.Redirect(http.StatusFound, "/")
}

// HandleLogout logs out the user
func (h *OAuthHandlers) HandleLogout(c *gin.Context) {
	// Get session store from authenticator
	_, sessionStore := h.getOIDCComponents()
	if sessionStore == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "OIDC not configured"})
		return
	}

	// Get session from cookie
	sessionID, err := auth.GetSessionCookie(c.Request)
	if err == nil && sessionID != "" {
		// Delete session from store
		sessionStore.DeleteSession(sessionID)
	}

	// Clear session cookie
	auth.ClearSessionCookie(c.Writer)

	// Return success or redirect
	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}

// getOIDCComponents retrieves the OIDC provider and session store from the authenticator
func (h *OAuthHandlers) getOIDCComponents() (*auth.OIDCProvider, *auth.SessionStore) {
	return h.authenticator.GetOIDCProvider(), h.authenticator.GetSessionStore()
}

// mapUserFromClaims maps JWT claims to internal username
func (h *OAuthHandlers) mapUserFromClaims(claims map[string]interface{}) (string, error) {
	return h.authenticator.MapUserFromClaims(claims)
}
