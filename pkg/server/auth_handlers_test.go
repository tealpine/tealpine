package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"tealpine/pkg/auth"
	"tealpine/pkg/config"
	"tealpine/pkg/server"
)

// oidcDiscoveryServer starts a minimal OIDC discovery server so that
// auth.NewAuthenticator can fully initialise the OIDC provider.
func oidcDiscoveryServer(t *testing.T) *httptest.Server {
	t.Helper()
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
				srvURL, srvURL+"/auth", srvURL+"/token", srvURL+"/jwks")
		case "/jwks":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"keys":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	srvURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// newOIDCAuthenticator creates an authenticator with a live OIDC provider
// backed by the supplied fake discovery server.
func newOIDCAuthenticator(t *testing.T, srv *httptest.Server) *auth.Authenticator {
	t.Helper()
	a := auth.NewAuthenticator(nil, &config.AuthConfig{
		Type:         "oidc",
		IssuerURL:    srv.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL+"/callback")
	require.NotNil(t, a.GetSessionStore(), "OIDC provider must initialise cleanly")
	return a
}

// sessionCookieName is the well-known cookie name used by the auth package.
const sessionCookieName = "tealpine_session"

func addSessionCookie(req *http.Request, sessionID string) {
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
}

// --- HandleLogin ---

func TestHandleLogin_OIDCNotConfigured(t *testing.T) {
	a := auth.NewAuthenticator(nil, nil, "")
	h := server.NewOAuthHandlers(a, false)
	c, w := ginCtx("GET", "/auth/login")
	h.HandleLogin(c)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "OIDC not configured")
}

func TestHandleLogin_RedirectsWithPKCEParams(t *testing.T) {
	srv := oidcDiscoveryServer(t)
	a := newOIDCAuthenticator(t, srv)
	h := server.NewOAuthHandlers(a, false)

	c, w := ginCtx("GET", "/auth/login")
	h.HandleLogin(c)

	require.Equal(t, http.StatusFound, w.Code)

	location := w.Header().Get("Location")
	require.True(t, strings.HasPrefix(location, srv.URL+"/auth"),
		"redirect should point to auth endpoint, got: %s", location)
	require.Contains(t, location, "code_challenge_method=S256")
	require.Contains(t, location, "code_challenge=")
	require.Contains(t, location, "state=")

	// A session cookie must be set so the callback can retrieve the state.
	require.NotEmpty(t, w.Header().Get("Set-Cookie"))
}

// --- HandleCallback ---

func TestHandleCallback_OIDCNotConfigured(t *testing.T) {
	a := auth.NewAuthenticator(nil, nil, "")
	h := server.NewOAuthHandlers(a, false)
	c, w := ginCtx("GET", "/auth/callback?code=x&state=y")
	h.HandleCallback(c)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "OIDC not configured")
}

func TestHandleCallback_MissingCode(t *testing.T) {
	srv := oidcDiscoveryServer(t)
	a := newOIDCAuthenticator(t, srv)
	h := server.NewOAuthHandlers(a, false)

	c, w := ginCtx("GET", "/auth/callback?state=mystate")
	h.HandleCallback(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "authorization code required")
}

func TestHandleCallback_MissingState(t *testing.T) {
	srv := oidcDiscoveryServer(t)
	a := newOIDCAuthenticator(t, srv)
	h := server.NewOAuthHandlers(a, false)

	c, w := ginCtx("GET", "/auth/callback?code=mycode")
	h.HandleCallback(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "state parameter required")
}

func TestHandleCallback_MissingSessionCookie(t *testing.T) {
	srv := oidcDiscoveryServer(t)
	a := newOIDCAuthenticator(t, srv)
	h := server.NewOAuthHandlers(a, false)

	c, w := ginCtx("GET", "/auth/callback?code=mycode&state=mystate")
	// No cookie set on request
	h.HandleCallback(c)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "session not found")
}

func TestHandleCallback_InvalidSession(t *testing.T) {
	srv := oidcDiscoveryServer(t)
	a := newOIDCAuthenticator(t, srv)
	h := server.NewOAuthHandlers(a, false)

	c, w := ginCtx("GET", "/auth/callback?code=mycode&state=mystate")
	addSessionCookie(c.Request, "nonexistent-session-id")
	h.HandleCallback(c)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "invalid session")
}

func TestHandleCallback_StateMismatch(t *testing.T) {
	srv := oidcDiscoveryServer(t)
	a := newOIDCAuthenticator(t, srv)
	h := server.NewOAuthHandlers(a, false)

	store := a.GetSessionStore()
	sessionID, err := store.CreateSession("")
	require.NoError(t, err)
	require.NoError(t, store.StoreOAuthState(sessionID, "correct-state", "verifier"))

	c, w := ginCtx("GET", "/auth/callback?code=mycode&state=wrong-state")
	addSessionCookie(c.Request, sessionID)
	h.HandleCallback(c)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "CSRF validation failed")
}

// --- HandleLogout ---

func TestHandleLogout_OIDCNotConfigured(t *testing.T) {
	a := auth.NewAuthenticator(nil, nil, "")
	h := server.NewOAuthHandlers(a, false)
	c, w := ginCtx("POST", "/auth/logout")
	h.HandleLogout(c)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "OIDC not configured")
}

func TestHandleLogout_NoSessionCookie(t *testing.T) {
	srv := oidcDiscoveryServer(t)
	a := newOIDCAuthenticator(t, srv)
	h := server.NewOAuthHandlers(a, false)

	c, w := ginCtx("POST", "/auth/logout")
	h.HandleLogout(c)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "logged out successfully", body["message"])
}

func TestHandleLogout_DeletesSession(t *testing.T) {
	srv := oidcDiscoveryServer(t)
	a := newOIDCAuthenticator(t, srv)
	h := server.NewOAuthHandlers(a, false)

	store := a.GetSessionStore()
	sessionID, err := store.CreateSession("alice")
	require.NoError(t, err)

	// Verify session exists before logout
	_, ok := store.GetSession(sessionID)
	require.True(t, ok)

	c, w := ginCtx("POST", "/auth/logout")
	addSessionCookie(c.Request, sessionID)
	h.HandleLogout(c)
	require.Equal(t, http.StatusOK, w.Code)

	// Session must be gone after logout
	_, ok = store.GetSession(sessionID)
	require.False(t, ok, "session should be deleted after logout")
}
