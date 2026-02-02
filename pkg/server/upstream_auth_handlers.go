package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tealpine/pkg/auth"
	"tealpine/pkg/client"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// UpstreamOAuthHandlers handles upstream MCP OIDC login/callback flows
type UpstreamOAuthHandlers struct {
	clients      map[string]*client.Client
	tokenStore   *auth.TokenStore
	sessionStore *auth.SessionStore
	serverHost   string
}

// NewUpstreamOAuthHandlers creates a new UpstreamOAuthHandlers instance
func NewUpstreamOAuthHandlers(
	clients map[string]*client.Client,
	tokenStore *auth.TokenStore,
	sessionStore *auth.SessionStore,
	serverHost string,
) *UpstreamOAuthHandlers {
	return &UpstreamOAuthHandlers{
		clients:      clients,
		tokenStore:   tokenStore,
		sessionStore: sessionStore,
		serverHost:   serverHost,
	}
}

// redirectURL builds the callback URL for a given MCP name
func (h *UpstreamOAuthHandlers) redirectURL(name string) string {
	scheme := "http"
	if strings.HasPrefix(h.serverHost, "https") {
		scheme = "https"
	}
	host := h.serverHost
	return fmt.Sprintf("%s://%s/upstream/%s/auth/callback", scheme, host, name)
}

// HandleLogin initiates the upstream OAuth flow
func (h *UpstreamOAuthHandlers) HandleLogin(c *gin.Context) {
	name := c.Param("name")
	cl, ok := h.clients[name]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown MCP client"})
		return
	}

	if !cl.IsLoginRequired() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "login not required for this client"})
		return
	}

	authInfo := cl.GetAuthInfo()
	if authInfo == nil {
		// Discovery failed during init — retry now
		cfg := cl.GetConfig()
		var clientIDOverride string
		if cfg.Auth != nil && cfg.Auth.ClientID != "" {
			clientIDOverride = cfg.Auth.ClientID
		}
		redirectURL := h.redirectURL(name)
		var err error
		authInfo, err = auth.DiscoverAndRegister(cfg.URL, redirectURL, clientIDOverride)
		if err != nil {
			logrus.Errorf("auth discovery retry failed for %s: %v", name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "auth discovery failed: " + err.Error()})
			return
		}
	}

	// If no ClientID yet (discovery passed empty redirect), do dynamic registration now
	if authInfo.ClientID == "" {
		redirectURL := h.redirectURL(name)
		if authInfo.RegistrationEndpoint != "" {
			clientID, err := auth.RegisterClient(authInfo.RegistrationEndpoint, redirectURL)
			if err != nil {
				logrus.Errorf("dynamic client registration failed for %s: %v", name, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "client registration failed"})
				return
			}
			authInfo.ClientID = clientID
			// Persist the client_id so we don't re-register on restart
			if err := h.tokenStore.Save(name, auth.StoredToken{ClientID: clientID}); err != nil {
				logrus.Warnf("failed to persist client_id for %s: %v", name, err)
			}
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no client_id and no registration endpoint"})
			return
		}
	}

	// Generate PKCE verifier and CSRF state
	codeVerifier, err := auth.GeneratePKCEVerifier()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate PKCE verifier"})
		return
	}
	state, err := auth.GenerateStateToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state token"})
		return
	}

	// Create or get session (validate it still exists — store is in-memory, may be lost on restart)
	sessionID, _ := auth.GetSessionCookie(c.Request)
	if sessionID != "" {
		if _, ok := h.sessionStore.GetSession(sessionID); !ok {
			sessionID = ""
		}
	}
	if sessionID == "" {
		var err error
		sessionID, err = h.sessionStore.CreateSession("")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
			return
		}
		auth.SetSessionCookie(c.Writer, sessionID)
	}

	if err := h.sessionStore.StoreOAuthState(sessionID, state, codeVerifier); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store OAuth state"})
		return
	}

	// Build authorization URL
	redirectURL := h.redirectURL(name)
	challenge := computePKCEChallenge(codeVerifier)

	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {authInfo.ClientID},
		"redirect_uri":          {redirectURL},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {"openid profile email"},
	}
	authURL := authInfo.AuthorizationEndpoint + "?" + params.Encode()

	c.Redirect(http.StatusFound, authURL)
}

// HandleCallback handles the OAuth callback from the upstream auth server
func (h *UpstreamOAuthHandlers) HandleCallback(c *gin.Context) {
	name := c.Param("name")
	cl, ok := h.clients[name]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown MCP client"})
		return
	}

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

	// Validate session and CSRF
	sessionID, err := auth.GetSessionCookie(c.Request)
	if err != nil || sessionID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	storedState, codeVerifier, err := h.sessionStore.GetOAuthState(sessionID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid session"})
		return
	}
	if storedState != state {
		c.JSON(http.StatusForbidden, gin.H{"error": "CSRF validation failed"})
		return
	}

	authInfo := cl.GetAuthInfo()
	if authInfo == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "auth info not available"})
		return
	}

	// Exchange code for tokens (public client — no client_secret)
	redirectURL := h.redirectURL(name)
	tokenResp, err := exchangeCodeForToken(authInfo.TokenEndpoint, code, codeVerifier, authInfo.ClientID, redirectURL)
	if err != nil {
		logrus.Errorf("token exchange failed for %s: %v", name, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token exchange failed"})
		return
	}

	// Save token to store
	storedToken := auth.StoredToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Expiry:       tokenResp.Expiry,
		ClientID:     authInfo.ClientID,
	}
	if err := h.tokenStore.Save(name, storedToken); err != nil {
		logrus.Warnf("failed to save token for %s: %v", name, err)
	}

	// Update client bearer token — triggers reconnection
	cl.SetBearerToken(tokenResp.AccessToken)

	// Clean up session
	h.sessionStore.DeleteSession(sessionID)
	auth.ClearSessionCookie(c.Writer)

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": fmt.Sprintf("Successfully authenticated upstream MCP '%s'", name),
	})
}

// computePKCEChallenge computes the S256 PKCE challenge from a code verifier
func computePKCEChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// tokenResponse represents the OAuth token endpoint response
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Expiry       time.Time
}

// exchangeCodeForToken exchanges an authorization code for tokens at the token endpoint
func exchangeCodeForToken(tokenEndpoint, code, codeVerifier, clientID, redirectURL string) (*tokenResponse, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURL},
		"client_id":     {clientID},
		"code_verifier": {codeVerifier},
	}

	resp, err := http.Post(tokenEndpoint, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("no access_token in token response")
	}

	// Compute expiry from expires_in if present
	if tokenResp.ExpiresIn > 0 {
		tokenResp.Expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return &tokenResp, nil
}
