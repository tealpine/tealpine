package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAuth_NoRules(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
	}

	auth, err := NewAuth(users, []AuthRule{})
	require.NoError(t, err)
	require.NotNil(t, auth)
	require.False(t, auth.enabled, "Auth should be disabled when no rules provided")
}

func TestNewAuth_WithRules(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
		"bob": {
			Token:  "bobtoken",
			Groups: []string{"gr1", "gr3"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"temp_*"},
		},
		{
			Group:  "gr2",
			Method: "tools/call",
			Allow:  []string{"calc_calculate"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)
	require.NotNil(t, auth)
	require.True(t, auth.enabled, "Auth should be enabled when rules provided")
	require.Equal(t, 2, len(auth.userTokens))
	require.Equal(t, "alice", auth.userTokens["alicetoken"])
	require.Equal(t, "bob", auth.userTokens["bobtoken"])
}

func TestMiddleware_Disabled(t *testing.T) {
	// Create auth with no rules (disabled)
	auth, err := NewAuth(make(map[string]*UserInfo), []AuthRule{})
	require.NoError(t, err)

	// Create test handler
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Create request without auth header
	mcpReq := MCPRequest{
		Method: "tools/list",
	}
	mcpReq.Params.Name = "calculate"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))

	// Record response
	rr := httptest.NewRecorder()

	// Call middleware
	auth.Middleware(handler).ServeHTTP(rr, req)

	// Should pass through without auth check
	require.True(t, called, "Handler should be called")
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestMiddleware_MissingAuthHeader(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"*"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mcpReq := MCPRequest{
		Method: "tools/list",
	}
	mcpReq.Params.Name = "calculate"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Contains(t, rr.Body.String(), "missing authorization header")
}

func TestMiddleware_InvalidAuthHeaderFormat(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"*"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mcpReq := MCPRequest{
		Method: "tools/list",
	}
	mcpReq.Params.Name = "calculate"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "InvalidFormat")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Contains(t, rr.Body.String(), "invalid authorization header format")
}

func TestMiddleware_InvalidToken(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"*"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mcpReq := MCPRequest{
		Method: "tools/list",
	}
	mcpReq.Params.Name = "calculate"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer invalidtoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Contains(t, rr.Body.String(), "invalid token")
}

func TestMiddleware_UserDirectPermission_Allowed(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
		"carol": {
			Token:  "caroltoken",
			Groups: []string{}, // No group membership
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"temp_*"},
		},
		{
			User:   "carol",
			Method: "tools/list",
			Allow:  []string{"temp_*"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	mcpReq := MCPRequest{
		Method: "tools/list",
	}
	mcpReq.Params.Name = "temp_get_room_temperature"
	body, _ := json.Marshal(mcpReq)

	// Test alice (has groups)
	aliceCalled := false
	aliceHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aliceCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alicetoken")

	rr := httptest.NewRecorder()
	auth.Middleware(aliceHandler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.True(t, aliceCalled, "Handler should be called for alice")

	// Test carol (no groups)
	carolCalled := false
	carolHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		carolCalled = true
		w.WriteHeader(http.StatusOK)
	})

	body, _ = json.Marshal(mcpReq)
	req = httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer caroltoken")

	rr = httptest.NewRecorder()
	auth.Middleware(carolHandler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.True(t, carolCalled, "Handler should be called for carol (no groups)")
}

func TestMiddleware_UserDirectPermission_Denied(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"temp_*"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mcpReq := MCPRequest{
		Method: "tools/list",
	}
	mcpReq.Params.Name = "calc_calculate"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alicetoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusForbidden, rr.Code)
	require.Contains(t, rr.Body.String(), "access denied")
}

func TestMiddleware_GroupPermission_Allowed(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
		"bob": {
			Token:  "bobtoken",
			Groups: []string{"gr1", "gr3"},
		},
	}

	authRules := []AuthRule{
		{
			Group:  "gr2",
			Method: "tools/call",
			Allow:  []string{"calc_calculate"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	// Test alice (member of gr2) - should be allowed
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mcpReq := MCPRequest{
		Method: "tools/call",
	}
	mcpReq.Params.Name = "calc_calculate"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alicetoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.True(t, called, "Handler should be called for alice")
}

func TestMiddleware_GroupPermission_Denied(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
		"bob": {
			Token:  "bobtoken",
			Groups: []string{"gr1", "gr3"},
		},
	}

	authRules := []AuthRule{
		{
			Group:  "gr2",
			Method: "tools/call",
			Allow:  []string{"calc_calculate"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	// Test bob (not member of gr2) - should be denied
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mcpReq := MCPRequest{
		Method: "tools/call",
	}
	mcpReq.Params.Name = "calc_calculate"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer bobtoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusForbidden, rr.Code)
	require.Contains(t, rr.Body.String(), "access denied")
}

func TestMiddleware_WildcardPattern(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"temp_*"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Test matching pattern
	testCases := []struct {
		name     string
		toolName string
		allowed  bool
	}{
		{"temp_get_room_temperature", "temp_get_room_temperature", true},
		{"temp_get_all_temperatures", "temp_get_all_temperatures", true},
		{"temp_any", "temp_any", true},
		{"calc_calculate", "calc_calculate", false},
		{"hello_world", "hello_world", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mcpReq := MCPRequest{
				Method: "tools/list",
			}
			mcpReq.Params.Name = tc.toolName
			body, _ := json.Marshal(mcpReq)
			req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer alicetoken")

			rr := httptest.NewRecorder()
			auth.Middleware(handler).ServeHTTP(rr, req)

			if tc.allowed {
				require.Equal(t, http.StatusOK, rr.Code, "Should allow %s", tc.toolName)
			} else {
				require.Equal(t, http.StatusForbidden, rr.Code, "Should deny %s", tc.toolName)
			}
		})
	}
}

func TestMiddleware_MethodSpecific(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"calculate"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Test with allowed method
	mcpReq := MCPRequest{
		Method: "tools/list",
	}
	mcpReq.Params.Name = "calculate"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alicetoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "Should allow tools/list")

	// Test with different method (same tool)
	mcpReq = MCPRequest{
		Method: "tools/call",
	}
	mcpReq.Params.Name = "calculate"
	body, _ = json.Marshal(mcpReq)
	req = httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alicetoken")

	rr = httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code, "Should deny tools/call")
}

func TestMiddleware_MultipleRules(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"temp_*"},
		},
		{
			Group:  "gr2",
			Method: "tools/list",
			Allow:  []string{"calc_calculate"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Test temp_* (from user rule)
	mcpReq := MCPRequest{
		Method: "tools/list",
	}
	mcpReq.Params.Name = "temp_get_room_temperature"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alicetoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "Should allow temp_*")

	// Test calc_calculate (from group rule)
	mcpReq = MCPRequest{
		Method: "tools/list",
	}
	mcpReq.Params.Name = "calc_calculate"
	body, _ = json.Marshal(mcpReq)
	req = httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alicetoken")

	rr = httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "Should allow calc_calculate")
}

func TestMiddleware_InvalidJSON(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"*"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Authorization", "Bearer alicetoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "failed to parse MCP request")
}

func TestMiddleware_UserGroupNameConflict(t *testing.T) {
	// Test that a user and group with the same name don't conflict
	users := map[string]*UserInfo{
		"admin": {
			Token:  "admintoken",
			Groups: []string{"admin"}, // User "admin" is in group "admin"
		},
		"john": {
			Token:  "johntoken",
			Groups: []string{"admin"}, // User "john" is also in group "admin"
		},
	}

	authRules := []AuthRule{
		{
			User:   "admin", // Direct permission for user "admin"
			Method: "tools/call",
			Allow:  []string{"user_tool"},
		},
		{
			Group:  "admin", // Group permission for group "admin"
			Method: "tools/call",
			Allow:  []string{"group_tool"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Test user "admin" - should have access to both user_tool (direct) and group_tool (via group)
	mcpReq := MCPRequest{
		Method: "tools/call",
	}
	mcpReq.Params.Name = "user_tool"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admintoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "User 'admin' should access user_tool (direct permission)")

	mcpReq.Params.Name = "group_tool"
	body, _ = json.Marshal(mcpReq)
	req = httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admintoken")

	rr = httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "User 'admin' should access group_tool (group permission)")

	// Test user "john" - should only have access to group_tool (via group "admin"), not user_tool
	mcpReq.Params.Name = "user_tool"
	body, _ = json.Marshal(mcpReq)
	req = httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer johntoken")

	rr = httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code, "User 'john' should NOT access user_tool (no direct permission)")

	mcpReq.Params.Name = "group_tool"
	body, _ = json.Marshal(mcpReq)
	req = httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer johntoken")

	rr = httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "User 'john' should access group_tool (group permission)")
}

func TestMiddleware_BodyRestoration(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"*"},
		},
	}

	auth, err := NewAuth(users, authRules)
	require.NoError(t, err)

	// Handler that reads the body
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var mcpReq MCPRequest
		err := json.NewDecoder(r.Body).Decode(&mcpReq)
		require.NoError(t, err, "Should be able to read body in handler")
		require.Equal(t, "tools/list", mcpReq.Method)
		require.Equal(t, "calculate", mcpReq.Params.Name)
		w.WriteHeader(http.StatusOK)
	})

	mcpReq := MCPRequest{
		Method: "tools/list",
	}
	mcpReq.Params.Name = "calculate"
	body, _ := json.Marshal(mcpReq)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alicetoken")

	rr := httptest.NewRecorder()
	auth.Middleware(handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
}
