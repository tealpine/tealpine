package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"tealpine/pkg/auth"
	"tealpine/pkg/client"
	"tealpine/pkg/config"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ginCtxFor creates a gin context for white-box tests.
func ginCtxFor(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, w
}

// newUpstreamHandlers builds an UpstreamOAuthHandlers with a temp-backed token store.
func newUpstreamHandlers(t *testing.T, clients map[string]*client.Client, store *auth.SessionStore) *UpstreamOAuthHandlers {
	t.Helper()
	tokenStore := auth.NewTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	return NewUpstreamOAuthHandlers(clients, tokenStore, store, "localhost:8080", false)
}

// startFakeUpstreamForLogin creates a single server that acts as both the MCP server
// (returns 401) and the authorization server (serves discovery + registration).
func startFakeUpstreamForLogin(t *testing.T) *httptest.Server {
	t.Helper()
	var srvURL string
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"authorization_servers":[%q]}`, srvURL)
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"authorization_endpoint":%q,"token_endpoint":%q,"registration_endpoint":%q}`,
			srvURL+"/auth", srvURL+"/token", srvURL+"/register")
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"client_id":"registered-client"}`)
	})
	// Everything else (including the MCP probe) returns 401.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	srv := httptest.NewServer(mux)
	srvURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// startClientWithLoginRequired initialises a client against the given upstream URL
// and waits until it emits EventLoginRequired.
func startClientWithLoginRequired(t *testing.T, upstreamURL string) *client.Client {
	t.Helper()
	cl := client.NewClient(config.UpstreamConfig{
		Name:      "svc",
		Transport: "streamablehttp",
		URL:       upstreamURL,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	cl.Start(ctx)
	_, err := cl.WaitForEvent(ctx, client.EventLoginRequired)
	require.NoError(t, err)
	return cl
}

// --- computePKCEChallenge ---

func TestUpstream_ComputePKCEChallenge_RFC7636Vector(t *testing.T) {
	// Test vector from RFC 7636 Appendix B.
	require.Equal(t,
		"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		computePKCEChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"),
	)
}

func TestUpstream_ComputePKCEChallenge_Deterministic(t *testing.T) {
	v := "some-verifier"
	require.Equal(t, computePKCEChallenge(v), computePKCEChallenge(v))
}

// --- redirectURL ---

func TestRedirectURL_HTTP(t *testing.T) {
	h := &UpstreamOAuthHandlers{serverHost: "localhost:8080"}
	require.Equal(t,
		"http://localhost:8080/upstream/myserver/auth/callback",
		h.redirectURL("myserver"),
	)
}

func TestRedirectURL_HTTPSPrefix(t *testing.T) {
	// The branch triggers for any serverHost string that starts with "https".
	h := &UpstreamOAuthHandlers{serverHost: "https-proxy.example.com:443"}
	got := h.redirectURL("svc")
	require.True(t, strings.HasPrefix(got, "https://"),
		"expected https scheme for host starting with 'https', got: %s", got)
}

// --- exchangeCodeForToken ---

func TestExchangeCodeForToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Equal(t, "authorization_code", r.FormValue("grant_type"))
		require.Equal(t, "mycode", r.FormValue("code"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"tok","token_type":"Bearer","expires_in":3600,"refresh_token":"rtok"}`)
	}))
	t.Cleanup(srv.Close)

	resp, err := exchangeCodeForToken(srv.URL, "mycode", "verifier", "client-id", "https://app/callback")
	require.NoError(t, err)
	require.Equal(t, "tok", resp.AccessToken)
	require.Equal(t, "Bearer", resp.TokenType)
	require.Equal(t, "rtok", resp.RefreshToken)
	require.False(t, resp.Expiry.IsZero(), "expiry should be populated from expires_in")
}

func TestExchangeCodeForToken_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
	}))
	t.Cleanup(srv.Close)

	_, err := exchangeCodeForToken(srv.URL, "bad-code", "v", "c", "https://app/callback")
	require.Error(t, err)
	require.Contains(t, err.Error(), "400")
}

func TestExchangeCodeForToken_MissingAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"token_type":"Bearer"}`)
	}))
	t.Cleanup(srv.Close)

	_, err := exchangeCodeForToken(srv.URL, "code", "v", "c", "https://app/callback")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no access_token")
}

func TestExchangeCodeForToken_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	t.Cleanup(srv.Close)

	_, err := exchangeCodeForToken(srv.URL, "code", "v", "c", "https://app/callback")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse token response")
}

// --- HandleLogin ---

func TestHandleLogin_ClientNotFound(t *testing.T) {
	h := newUpstreamHandlers(t, map[string]*client.Client{}, auth.NewSessionStore())
	c, w := ginCtxFor("GET", "/upstream/ghost/auth/login")
	c.Params = gin.Params{{Key: "name", Value: "ghost"}}
	h.HandleLogin(c)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleLogin_LoginNotRequired(t *testing.T) {
	// NewClient has loginRequired = false by default.
	cl := client.NewClient(config.UpstreamConfig{
		Name:      "svc",
		Transport: "streamablehttp",
		URL:       "http://example.com/mcp",
	})
	h := newUpstreamHandlers(t, map[string]*client.Client{"svc": cl}, auth.NewSessionStore())
	c, w := ginCtxFor("GET", "/upstream/svc/auth/login")
	c.Params = gin.Params{{Key: "name", Value: "svc"}}
	h.HandleLogin(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "login not required")
}

func TestHandleLogin_DiscoveryRetryFails(t *testing.T) {
	// Fake MCP server returns 401 for all paths — OIDC discovery will also fail.
	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(mcpSrv.Close)

	cl := startClientWithLoginRequired(t, mcpSrv.URL)
	// authInfo is nil because discovery failed; HandleLogin retry will also fail.

	h := newUpstreamHandlers(t, map[string]*client.Client{"svc": cl}, auth.NewSessionStore())
	c, w := ginCtxFor("GET", "/upstream/svc/auth/login")
	c.Params = gin.Params{{Key: "name", Value: "svc"}}
	h.HandleLogin(c)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "auth discovery failed")
}

func TestHandleLogin_RedirectsWithPKCEParams(t *testing.T) {
	srv := startFakeUpstreamForLogin(t)
	cl := startClientWithLoginRequired(t, srv.URL)
	// authInfo is set but ClientID is empty (no redirect URL during init discovery).
	// HandleLogin will call RegisterClient to obtain one.

	h := newUpstreamHandlers(t, map[string]*client.Client{"svc": cl}, auth.NewSessionStore())
	c, w := ginCtxFor("GET", "/upstream/svc/auth/login")
	c.Params = gin.Params{{Key: "name", Value: "svc"}}
	h.HandleLogin(c)

	require.Equal(t, http.StatusFound, w.Code)
	location := w.Header().Get("Location")
	require.True(t, strings.HasPrefix(location, srv.URL+"/auth"),
		"redirect must point to authorization endpoint, got: %s", location)
	require.Contains(t, location, "code_challenge_method=S256")
	require.Contains(t, location, "code_challenge=")
	require.Contains(t, location, "state=")
	require.Contains(t, location, "client_id=registered-client")
	require.NotEmpty(t, w.Header().Get("Set-Cookie"), "session cookie must be set")
}

// --- HandleCallback ---

func TestHandleCallback_ClientNotFound(t *testing.T) {
	h := newUpstreamHandlers(t, map[string]*client.Client{}, auth.NewSessionStore())
	c, w := ginCtxFor("GET", "/upstream/ghost/auth/callback?code=x&state=y")
	c.Params = gin.Params{{Key: "name", Value: "ghost"}}
	h.HandleCallback(c)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleCallback_MissingCode(t *testing.T) {
	cl := client.NewClient(config.UpstreamConfig{Name: "svc", Transport: "streamablehttp"})
	h := newUpstreamHandlers(t, map[string]*client.Client{"svc": cl}, auth.NewSessionStore())
	c, w := ginCtxFor("GET", "/upstream/svc/auth/callback?state=mystate")
	c.Params = gin.Params{{Key: "name", Value: "svc"}}
	h.HandleCallback(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "authorization code required")
}

func TestHandleCallback_MissingState(t *testing.T) {
	cl := client.NewClient(config.UpstreamConfig{Name: "svc", Transport: "streamablehttp"})
	h := newUpstreamHandlers(t, map[string]*client.Client{"svc": cl}, auth.NewSessionStore())
	c, w := ginCtxFor("GET", "/upstream/svc/auth/callback?code=mycode")
	c.Params = gin.Params{{Key: "name", Value: "svc"}}
	h.HandleCallback(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "state parameter required")
}

func TestHandleCallback_MissingSessionCookie(t *testing.T) {
	cl := client.NewClient(config.UpstreamConfig{Name: "svc", Transport: "streamablehttp"})
	h := newUpstreamHandlers(t, map[string]*client.Client{"svc": cl}, auth.NewSessionStore())
	c, w := ginCtxFor("GET", "/upstream/svc/auth/callback?code=x&state=y")
	c.Params = gin.Params{{Key: "name", Value: "svc"}}
	h.HandleCallback(c)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "session not found")
}

func TestHandleCallback_InvalidSession(t *testing.T) {
	cl := client.NewClient(config.UpstreamConfig{Name: "svc", Transport: "streamablehttp"})
	h := newUpstreamHandlers(t, map[string]*client.Client{"svc": cl}, auth.NewSessionStore())
	c, w := ginCtxFor("GET", "/upstream/svc/auth/callback?code=x&state=y")
	c.Params = gin.Params{{Key: "name", Value: "svc"}}
	c.Request.AddCookie(&http.Cookie{Name: "tealpine_session", Value: "nonexistent-id"})
	h.HandleCallback(c)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "invalid session")
}

func TestHandleCallback_StateMismatch(t *testing.T) {
	store := auth.NewSessionStore()
	sessionID, err := store.CreateSession("")
	require.NoError(t, err)
	require.NoError(t, store.StoreOAuthState(sessionID, "correct-state", "verifier"))

	cl := client.NewClient(config.UpstreamConfig{Name: "svc", Transport: "streamablehttp"})
	h := newUpstreamHandlers(t, map[string]*client.Client{"svc": cl}, store)
	c, w := ginCtxFor("GET", "/upstream/svc/auth/callback?code=x&state=wrong-state")
	c.Params = gin.Params{{Key: "name", Value: "svc"}}
	c.Request.AddCookie(&http.Cookie{Name: "tealpine_session", Value: sessionID})
	h.HandleCallback(c)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "CSRF validation failed")
}

func TestHandleCallback_AuthInfoNil(t *testing.T) {
	// Fresh client has authInfo == nil. Once CSRF passes the handler returns 500.
	store := auth.NewSessionStore()
	sessionID, err := store.CreateSession("")
	require.NoError(t, err)
	require.NoError(t, store.StoreOAuthState(sessionID, "mystate", "verifier"))

	cl := client.NewClient(config.UpstreamConfig{Name: "svc", Transport: "streamablehttp"})
	h := newUpstreamHandlers(t, map[string]*client.Client{"svc": cl}, store)
	c, w := ginCtxFor("GET", "/upstream/svc/auth/callback?code=x&state=mystate")
	c.Params = gin.Params{{Key: "name", Value: "svc"}}
	c.Request.AddCookie(&http.Cookie{Name: "tealpine_session", Value: sessionID})
	h.HandleCallback(c)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "auth info not available")
}
