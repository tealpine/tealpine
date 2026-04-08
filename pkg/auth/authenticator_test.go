package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"tealpine/pkg/config"
)

// newAuthenticatorWithUsers builds a minimal authenticator with static tokens and no OIDC.
func newAuthenticatorWithUsers(users map[string]config.UserConfig) *Authenticator {
	return NewAuthenticator(users, nil, "")
}

func TestNewAuthenticator_NoUsers(t *testing.T) {
	auth := NewAuthenticator(nil, nil, "")
	require.NotNil(t, auth)
	require.False(t, auth.enabled, "Authenticator should be disabled when no users provided")
}

func TestNewAuthenticator_WithUsers(t *testing.T) {
	users := map[string]config.UserConfig{
		"alice": {
			Token: "alicetoken",
		},
		"bob": {
			Token: "bobtoken",
		},
	}

	auth := NewAuthenticator(users, nil, "")
	require.NotNil(t, auth)
	require.True(t, auth.enabled, "Authenticator should be enabled when users provided")
	require.Equal(t, 2, len(auth.userTokens))
	require.Equal(t, "alice", auth.userTokens["alicetoken"])
	require.Equal(t, "bob", auth.userTokens["bobtoken"])
}

func TestAuthenticatorMiddleware_Disabled(t *testing.T) {
	// Create authenticator with no users (disabled)
	auth := NewAuthenticator(nil, nil, "")

	// Create test handler
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Create request without auth header
	req := httptest.NewRequest("POST", "/test", nil)

	// Record response
	rr := httptest.NewRecorder()

	// Call middleware
	auth.Middleware(handler).ServeHTTP(rr, req)

	// Should pass through without auth check
	require.True(t, called, "Handler should be called")
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestAuthenticatorMiddleware_MissingAuthHeader(t *testing.T) {
	users := map[string]config.UserConfig{
		"alice": {
			Token: "alicetoken",
		},
	}

	auth := NewAuthenticator(users, nil, "")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", nil)

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Contains(t, rr.Body.String(), "missing authorization header")
}

func TestAuthenticatorMiddleware_InvalidAuthHeaderFormat(t *testing.T) {
	users := map[string]config.UserConfig{
		"alice": {
			Token: "alicetoken",
		},
	}

	auth := NewAuthenticator(users, nil, "")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Authorization", "InvalidFormat")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Contains(t, rr.Body.String(), "invalid authorization header format")
}

func TestAuthenticatorMiddleware_InvalidToken(t *testing.T) {
	users := map[string]config.UserConfig{
		"alice": {
			Token: "alicetoken",
		},
	}

	auth := NewAuthenticator(users, nil, "")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalidtoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Contains(t, rr.Body.String(), "invalid token")
}

func TestAuthenticatorMiddleware_ValidToken(t *testing.T) {
	users := map[string]config.UserConfig{
		"alice": {
			Token: "alicetoken",
		},
	}

	auth := NewAuthenticator(users, nil, "")

	// Handler that checks if username is in context
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		username, ok := r.Context().Value(CtxUsernameKey).(string)
		require.True(t, ok, "Username should be in context")
		require.Equal(t, "alice", username)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Authorization", "Bearer alicetoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.True(t, called, "Handler should be called")
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestAuthenticatorMiddleware_MultipleUsers(t *testing.T) {
	users := map[string]config.UserConfig{
		"alice": {
			Token: "alicetoken",
		},
		"bob": {
			Token: "bobtoken",
		},
	}

	auth := NewAuthenticator(users, nil, "")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, ok := r.Context().Value(CtxUsernameKey).(string)
		require.True(t, ok)
		w.Header().Set("X-Username", username)
		w.WriteHeader(http.StatusOK)
	})

	// Test alice
	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Authorization", "Bearer alicetoken")
	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "alice", rr.Header().Get("X-Username"))

	// Test bob
	req = httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("Authorization", "Bearer bobtoken")
	rr = httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "bob", rr.Header().Get("X-Username"))
}

// --- looksLikeJWT ---

func TestLooksLikeJWT(t *testing.T) {
	require.True(t, looksLikeJWT("header.payload.signature"))
	require.False(t, looksLikeJWT("notajwt"))
	require.False(t, looksLikeJWT("only.two"))
	require.False(t, looksLikeJWT("a.b.c.d"))
	require.False(t, looksLikeJWT(""))
}

// --- isValidGroupName ---

func TestIsValidGroupName(t *testing.T) {
	valid := []string{"admins", "my-group", "my_group", "Group123", "A"}
	for _, name := range valid {
		require.True(t, isValidGroupName(name), "expected valid: %q", name)
	}

	invalid := []string{"group name", "group/slash", "group@domain", "", "gr!oup"}
	for _, name := range invalid {
		require.False(t, isValidGroupName(name), "expected invalid: %q", name)
	}
}

// --- checkSession via Middleware ---

// authenticatorWithSession builds an authenticator that has a session store but no OIDC.
func authenticatorWithSession(t *testing.T) (*Authenticator, *SessionStore) {
	t.Helper()
	store := NewSessionStore()
	a := &Authenticator{
		sessionStore: store,
		userTokens:   map[string]string{},
		users:        map[string]bool{},
		enabled:      true,
	}
	return a, store
}

func TestMiddleware_SessionAuth_Valid(t *testing.T) {
	a, store := authenticatorWithSession(t)
	sessionID, err := store.CreateSession("alice")
	require.NoError(t, err)

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		username, ok := r.Context().Value(CtxUsernameKey).(string)
		require.True(t, ok)
		require.Equal(t, "alice", username)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})

	rr := httptest.NewRecorder()
	a.Middleware(handler).ServeHTTP(rr, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestMiddleware_SessionAuth_UnknownSessionFallsThrough(t *testing.T) {
	a, _ := authenticatorWithSession(t)

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "nonexistent-session-id"})

	rr := httptest.NewRecorder()
	// No bearer token either — should get 401
	a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

// --- getUserFromJWT ---

func newAuthenticatorForClaims(authCfg *config.AuthConfig, users map[string]bool) *Authenticator {
	return &Authenticator{
		users:      users,
		userTokens: map[string]string{},
		authConfig: authCfg,
		enabled:    true,
	}
}

func TestGetUserFromJWT_StrictMode_UserFound(t *testing.T) {
	a := newAuthenticatorForClaims(
		&config.AuthConfig{RequireUserInConfig: true},
		map[string]bool{"alice@example.com": true},
	)
	username, groups, err := a.getUserFromJWT(map[string]interface{}{"email": "alice@example.com"})
	require.NoError(t, err)
	require.Equal(t, "alice@example.com", username)
	require.Nil(t, groups)
}

func TestGetUserFromJWT_StrictMode_UserNotInConfig(t *testing.T) {
	a := newAuthenticatorForClaims(
		&config.AuthConfig{RequireUserInConfig: true},
		map[string]bool{},
	)
	_, _, err := a.getUserFromJWT(map[string]interface{}{"email": "stranger@example.com"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found in configuration")
}

func TestGetUserFromJWT_MissingClaimField(t *testing.T) {
	a := newAuthenticatorForClaims(
		&config.AuthConfig{RequireUserInConfig: true},
		map[string]bool{},
	)
	_, _, err := a.getUserFromJWT(map[string]interface{}{"sub": "12345"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "'email' not found")
}

func TestGetUserFromJWT_CustomUserClaimField(t *testing.T) {
	a := newAuthenticatorForClaims(
		&config.AuthConfig{RequireUserInConfig: true, UserClaimField: "preferred_username"},
		map[string]bool{"bob": true},
	)
	username, _, err := a.getUserFromJWT(map[string]interface{}{"preferred_username": "bob"})
	require.NoError(t, err)
	require.Equal(t, "bob", username)
}

func TestGetUserFromJWT_OpenMode_AnyUserAllowed(t *testing.T) {
	a := newAuthenticatorForClaims(
		&config.AuthConfig{RequireUserInConfig: false},
		map[string]bool{},
	)
	username, _, err := a.getUserFromJWT(map[string]interface{}{"email": "anyone@example.com"})
	require.NoError(t, err)
	require.Equal(t, "anyone@example.com", username)
}

func TestGetUserFromJWT_OpenMode_ExtractsGroups(t *testing.T) {
	a := newAuthenticatorForClaims(
		&config.AuthConfig{RequireUserInConfig: false},
		map[string]bool{},
	)
	claims := map[string]interface{}{
		"email":  "alice@example.com",
		"groups": []interface{}{"admins", "devs"},
	}
	_, groups, err := a.getUserFromJWT(claims)
	require.NoError(t, err)
	require.Equal(t, []string{"admins", "devs"}, groups)
}

// --- extractGroupsFromClaims ---

func TestExtractGroupsFromClaims_StringArray(t *testing.T) {
	a := newAuthenticatorForClaims(&config.AuthConfig{}, map[string]bool{})
	groups := a.extractGroupsFromClaims(map[string]interface{}{
		"groups": []interface{}{"admins", "devs", "team-a"},
	})
	require.Equal(t, []string{"admins", "devs", "team-a"}, groups)
}

func TestExtractGroupsFromClaims_CommaSeparatedString(t *testing.T) {
	a := newAuthenticatorForClaims(&config.AuthConfig{}, map[string]bool{})
	groups := a.extractGroupsFromClaims(map[string]interface{}{
		"groups": "admins,devs,team-a",
	})
	require.Equal(t, []string{"admins", "devs", "team-a"}, groups)
}

func TestExtractGroupsFromClaims_InvalidNamesFiltered(t *testing.T) {
	a := newAuthenticatorForClaims(&config.AuthConfig{}, map[string]bool{})
	groups := a.extractGroupsFromClaims(map[string]interface{}{
		"groups": []interface{}{"admins", "invalid group", "devs"},
	})
	require.Equal(t, []string{"admins", "devs"}, groups)
}

func TestExtractGroupsFromClaims_MissingClaim(t *testing.T) {
	a := newAuthenticatorForClaims(&config.AuthConfig{}, map[string]bool{})
	groups := a.extractGroupsFromClaims(map[string]interface{}{"email": "alice@example.com"})
	require.Nil(t, groups)
}

func TestExtractGroupsFromClaims_CustomClaimField(t *testing.T) {
	a := newAuthenticatorForClaims(&config.AuthConfig{GroupClaimField: "roles"}, map[string]bool{})
	groups := a.extractGroupsFromClaims(map[string]interface{}{
		"roles": []interface{}{"admin", "viewer"},
	})
	require.Equal(t, []string{"admin", "viewer"}, groups)
}

// --- MapUserFromClaims ---

func TestMapUserFromClaims_ValidUser(t *testing.T) {
	a := newAuthenticatorForClaims(
		&config.AuthConfig{RequireUserInConfig: true},
		map[string]bool{"alice@example.com": true},
	)
	username, err := a.MapUserFromClaims(map[string]interface{}{"email": "alice@example.com"})
	require.NoError(t, err)
	require.Equal(t, "alice@example.com", username)
}

func TestMapUserFromClaims_UnknownUser(t *testing.T) {
	a := newAuthenticatorForClaims(
		&config.AuthConfig{RequireUserInConfig: true},
		map[string]bool{},
	)
	_, err := a.MapUserFromClaims(map[string]interface{}{"email": "unknown@example.com"})
	require.Error(t, err)
}
