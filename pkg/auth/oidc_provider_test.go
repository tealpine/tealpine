package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"tealpine/pkg/config"
)

// startFakeOIDC starts a minimal OIDC server covering discovery, JWKS, token,
// and userinfo. userinfoHandler overrides /userinfo (nil = endpoint absent).
func startFakeOIDC(t *testing.T, userinfoHandler http.HandlerFunc) *httptest.Server {
	t.Helper()

	var issuerURL string
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]interface{}{
			"issuer":                  issuerURL,
			"authorization_endpoint":  issuerURL + "/auth",
			"token_endpoint":          issuerURL + "/token",
			"jwks_uri":                issuerURL + "/jwks",
			"userinfo_endpoint":       issuerURL + "/userinfo",
			"response_types_supported": []string{"code"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(doc) //nolint:errcheck
	})

	// Return an empty key set — sufficient for provider initialisation;
	// JWT verification will fail (no matching key), which is expected in tests.
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"keys":[]}`)
	})

	// Always reject token exchanges so ExchangeCode tests get a clear error.
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid_client"}`)
	})

	if userinfoHandler != nil {
		mux.HandleFunc("/userinfo", userinfoHandler)
	}

	srv := httptest.NewServer(mux)
	issuerURL = srv.URL // set before any request is made
	t.Cleanup(srv.Close)
	return srv
}

// newFakeProvider creates an OIDCProvider backed by startFakeOIDC.
func newFakeProvider(t *testing.T, userinfoHandler http.HandlerFunc) (*OIDCProvider, *httptest.Server) {
	t.Helper()
	srv := startFakeOIDC(t, userinfoHandler)
	cfg := &config.AuthConfig{
		IssuerURL:    srv.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}
	p, err := NewOIDCProvider(cfg, srv.URL+"/callback")
	require.NoError(t, err)
	return p, srv
}

// --- computePKCEChallenge ---

func TestComputePKCEChallenge_RFC7636Vector(t *testing.T) {
	// Test vector from RFC 7636 Appendix B.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	require.Equal(t, want, computePKCEChallenge(verifier))
}

func TestComputePKCEChallenge_Deterministic(t *testing.T) {
	v := "some-verifier-string"
	require.Equal(t, computePKCEChallenge(v), computePKCEChallenge(v))
}

// --- GenerateAuthURL ---

func TestGenerateAuthURL_RequiredParams(t *testing.T) {
	p := &OIDCProvider{
		oauth2Config: oauth2.Config{
			ClientID:    "my-client",
			RedirectURL: "https://app.example.com/callback",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://auth.example.com/authorize",
				TokenURL: "https://auth.example.com/token",
			},
		},
	}

	verifier := "my-code-verifier"
	rawURL := p.GenerateAuthURL("my-state", verifier)

	u, err := url.Parse(rawURL)
	require.NoError(t, err)

	q := u.Query()
	require.Equal(t, "my-state", q.Get("state"))
	require.Equal(t, "code", q.Get("response_type"))
	require.Equal(t, "my-client", q.Get("client_id"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.Equal(t, computePKCEChallenge(verifier), q.Get("code_challenge"))
}

// --- NewOIDCProvider ---

func TestNewOIDCProvider_Success(t *testing.T) {
	srv := startFakeOIDC(t, nil)
	cfg := &config.AuthConfig{
		IssuerURL:    srv.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}
	p, err := NewOIDCProvider(cfg, srv.URL+"/callback")
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestNewOIDCProvider_DiscoveryFails(t *testing.T) {
	// Server returns 404 for the discovery document.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.AuthConfig{IssuerURL: srv.URL, ClientID: "test-client"}
	_, err := NewOIDCProvider(cfg, srv.URL+"/callback")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to create OIDC provider")
}

// --- ValidateJWT ---

func TestValidateJWT_MalformedToken(t *testing.T) {
	p, _ := newFakeProvider(t, nil)

	_, err := p.ValidateJWT(context.Background(), "not.a.valid.jwt.payload")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to verify JWT token")
}

// --- ExchangeCode ---

func TestExchangeCode_TokenEndpointError(t *testing.T) {
	// The fake server's /token always returns 401 invalid_client.
	p, _ := newFakeProvider(t, nil)

	_, err := p.ExchangeCode(context.Background(), "some-code", "some-verifier")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to exchange authorization code")
}

// --- IntrospectToken ---

func TestIntrospectToken_Success(t *testing.T) {
	userinfo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"sub":"user123","email":"alice@example.com","name":"Alice"}`)
	})

	p, _ := newFakeProvider(t, userinfo)
	claims, err := p.IntrospectToken(context.Background(), "opaque-token")
	require.NoError(t, err)
	require.Equal(t, "user123", claims["sub"])
	require.Equal(t, "alice@example.com", claims["email"])
	require.Equal(t, "Alice", claims["name"])
}

func TestIntrospectToken_UserinfoError(t *testing.T) {
	userinfo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})

	p, _ := newFakeProvider(t, userinfo)
	_, err := p.IntrospectToken(context.Background(), "bad-token")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to validate access token via userinfo")
}
