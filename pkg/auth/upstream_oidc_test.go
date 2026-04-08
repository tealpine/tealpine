package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// startFakeUpstream starts a server that serves all upstream OIDC endpoints on
// a single host, making it usable as both MCP server and authorization server.
func startFakeUpstream(t *testing.T, registrationStatus int, registrationClientID string) *httptest.Server {
	t.Helper()
	var srvURL string
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"authorization_servers": []string{srvURL},
		})
	})

	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"authorization_endpoint": srvURL + "/auth",
			"token_endpoint":         srvURL + "/token",
			"registration_endpoint":  srvURL + "/register",
		})
	})

	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(registrationStatus)
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"client_id": registrationClientID,
		})
	})

	srv := httptest.NewServer(mux)
	srvURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// --- DiscoverAuthServer ---

func TestDiscoverAuthServer_Success(t *testing.T) {
	authServerURL := "https://auth.example.com"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/.well-known/oauth-protected-resource", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"authorization_servers":[%q]}`, authServerURL)
	}))
	t.Cleanup(srv.Close)

	got, err := DiscoverAuthServer(srv.URL + "/mcp")
	require.NoError(t, err)
	require.Equal(t, authServerURL, got)
}

func TestDiscoverAuthServer_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, err := DiscoverAuthServer(srv.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
}

func TestDiscoverAuthServer_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	t.Cleanup(srv.Close)

	_, err := DiscoverAuthServer(srv.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse resource metadata")
}

func TestDiscoverAuthServer_EmptyAuthorizationServers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"authorization_servers":[]}`)
	}))
	t.Cleanup(srv.Close)

	_, err := DiscoverAuthServer(srv.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no authorization_servers")
}

func TestDiscoverAuthServer_InvalidURL(t *testing.T) {
	_, err := DiscoverAuthServer("://bad-url")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid MCP URL")
}

// --- DiscoverOIDCEndpoints ---

func endpointsJSON(authURL, tokenURL, registrationURL string) string {
	return fmt.Sprintf(
		`{"authorization_endpoint":%q,"token_endpoint":%q,"registration_endpoint":%q}`,
		authURL, tokenURL, registrationURL,
	)
}

func TestDiscoverOIDCEndpoints_RFC8414(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			fmt.Fprint(w, endpointsJSON("https://auth/authorize", "https://auth/token", ""))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	ep, err := DiscoverOIDCEndpoints(srv.URL)
	require.NoError(t, err)
	require.Equal(t, "https://auth/authorize", ep.AuthorizationEndpoint)
	require.Equal(t, "https://auth/token", ep.TokenEndpoint)
}

func TestDiscoverOIDCEndpoints_FallsBackToOpenIDConfiguration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			fmt.Fprint(w, endpointsJSON("https://auth/authorize", "https://auth/token", "https://auth/register"))
			return
		}
		http.NotFound(w, r) // RFC 8414 path → 404
	}))
	t.Cleanup(srv.Close)

	ep, err := DiscoverOIDCEndpoints(srv.URL)
	require.NoError(t, err)
	require.Equal(t, "https://auth/register", ep.RegistrationEndpoint)
}

func TestDiscoverOIDCEndpoints_BothEndpointsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, err := DiscoverOIDCEndpoints(srv.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no authorization server metadata found")
}

func TestDiscoverOIDCEndpoints_MissingAuthorizationEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"token_endpoint":"https://auth/token"}`)
	}))
	t.Cleanup(srv.Close)

	_, err := DiscoverOIDCEndpoints(srv.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no authorization server metadata found")
}

func TestDiscoverOIDCEndpoints_MissingTokenEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"authorization_endpoint":"https://auth/authorize"}`)
	}))
	t.Cleanup(srv.Close)

	_, err := DiscoverOIDCEndpoints(srv.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no authorization server metadata found")
}

// --- RegisterClient ---

func TestRegisterClient_Created(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"client_id":"my-client"}`)
	}))
	t.Cleanup(srv.Close)

	clientID, err := RegisterClient(srv.URL+"/register", "https://app.example.com/callback")
	require.NoError(t, err)
	require.Equal(t, "my-client", clientID)
}

func TestRegisterClient_OKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"client_id":"ok-client"}`)
	}))
	t.Cleanup(srv.Close)

	clientID, err := RegisterClient(srv.URL+"/register", "https://app.example.com/callback")
	require.NoError(t, err)
	require.Equal(t, "ok-client", clientID)
}

func TestRegisterClient_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_redirect_uri"}`)
	}))
	t.Cleanup(srv.Close)

	_, err := RegisterClient(srv.URL+"/register", "https://app.example.com/callback")
	require.Error(t, err)
	require.Contains(t, err.Error(), "400")
}

func TestRegisterClient_MissingClientID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"other_field":"value"}`)
	}))
	t.Cleanup(srv.Close)

	_, err := RegisterClient(srv.URL+"/register", "https://app.example.com/callback")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no client_id")
}

func TestRegisterClient_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	t.Cleanup(srv.Close)

	_, err := RegisterClient(srv.URL+"/register", "https://app.example.com/callback")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse registration response")
}

// --- DiscoverAndRegister ---

func TestDiscoverAndRegister_FullFlow(t *testing.T) {
	srv := startFakeUpstream(t, http.StatusCreated, "dynamic-client")

	info, err := DiscoverAndRegister(srv.URL+"/mcp", srv.URL+"/callback", "")
	require.NoError(t, err)
	require.Equal(t, srv.URL, info.AuthServerURL)
	require.Equal(t, srv.URL+"/auth", info.AuthorizationEndpoint)
	require.Equal(t, srv.URL+"/token", info.TokenEndpoint)
	require.Equal(t, "dynamic-client", info.ClientID)
}

func TestDiscoverAndRegister_WithClientIDOverride(t *testing.T) {
	srv := startFakeUpstream(t, http.StatusCreated, "should-not-be-used")

	info, err := DiscoverAndRegister(srv.URL+"/mcp", srv.URL+"/callback", "override-client")
	require.NoError(t, err)
	require.Equal(t, "override-client", info.ClientID)
}

func TestDiscoverAndRegister_NoRegistrationEndpoint(t *testing.T) {
	var srvURL string
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"authorization_servers": []string{srvURL}}) //nolint:errcheck
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		// No registration_endpoint
		fmt.Fprintf(w, `{"authorization_endpoint":"%s/auth","token_endpoint":"%s/token"}`, srvURL, srvURL)
	})

	srv := httptest.NewServer(mux)
	srvURL = srv.URL
	t.Cleanup(srv.Close)

	info, err := DiscoverAndRegister(srv.URL+"/mcp", srv.URL+"/callback", "")
	require.NoError(t, err)
	require.Empty(t, info.ClientID) // registration skipped
}

func TestDiscoverAndRegister_AuthServerDiscoveryFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, err := DiscoverAndRegister(srv.URL+"/mcp", "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "auth server discovery failed")
}

func TestDiscoverAndRegister_OIDCDiscoveryFails(t *testing.T) {
	var srvURL string
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"authorization_servers": []string{srvURL}}) //nolint:errcheck
	})
	// No oauth-authorization-server or openid-configuration endpoints

	srv := httptest.NewServer(mux)
	srvURL = srv.URL
	t.Cleanup(srv.Close)

	_, err := DiscoverAndRegister(srv.URL+"/mcp", "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "OIDC discovery failed")
}

func TestDiscoverAndRegister_RegistrationFails(t *testing.T) {
	srv := startFakeUpstream(t, http.StatusBadRequest, "")

	_, err := DiscoverAndRegister(srv.URL+"/mcp", srv.URL+"/callback", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "dynamic client registration failed")
}

// --- ProbeUpstreamAuth ---

func TestProbeUpstreamAuth_RequiresAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	requires, err := ProbeUpstreamAuth(srv.URL)
	require.NoError(t, err)
	require.True(t, requires)
}

func TestProbeUpstreamAuth_NoAuthRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	requires, err := ProbeUpstreamAuth(srv.URL)
	require.NoError(t, err)
	require.False(t, requires)
}

func TestProbeUpstreamAuth_OtherStatusNotAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	requires, err := ProbeUpstreamAuth(srv.URL)
	require.NoError(t, err)
	require.False(t, requires)
}

func TestProbeUpstreamAuth_ConnectionError(t *testing.T) {
	// Start then immediately close a server to get a refused-connection URL.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	_, err := ProbeUpstreamAuth(closedURL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to probe upstream server")
}
