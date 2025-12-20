package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"tealpine/pkg/config"
)

func TestNewAuthenticator_NoUsers(t *testing.T) {
	auth := NewAuthenticator(nil)
	require.NotNil(t, auth)
	require.False(t, auth.enabled, "Authenticator should be disabled when no users provided")
}

func TestNewAuthenticator_WithUsers(t *testing.T) {
	users := map[string]config.UserConfig{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
		"bob": {
			Token:  "bobtoken",
			Groups: []string{"gr1", "gr3"},
		},
	}

	auth := NewAuthenticator(users)
	require.NotNil(t, auth)
	require.True(t, auth.enabled, "Authenticator should be enabled when users provided")
	require.Equal(t, 2, len(auth.userTokens))
	require.Equal(t, "alice", auth.userTokens["alicetoken"])
	require.Equal(t, "bob", auth.userTokens["bobtoken"])
}

func TestAuthenticatorMiddleware_Disabled(t *testing.T) {
	// Create authenticator with no users (disabled)
	auth := NewAuthenticator(nil)

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
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	auth := NewAuthenticator(users)

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
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	auth := NewAuthenticator(users)

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
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	auth := NewAuthenticator(users)

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
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	auth := NewAuthenticator(users)

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
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
		"bob": {
			Token:  "bobtoken",
			Groups: []string{"gr2"},
		},
	}

	auth := NewAuthenticator(users)

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
