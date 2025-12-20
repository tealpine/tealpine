package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"tealpine/pkg/auth"
	"tealpine/pkg/config"
)

func TestAdminAuthorizer(t *testing.T) {
	users := map[string]config.UserConfig{
		"admin-user": {
			Token:  "admin-token",
			Groups: []string{},
		},
		"group-user": {
			Token:  "group-token",
			Groups: []string{"admin-group"},
		},
		"regular-user": {
			Token:  "regular-token",
			Groups: []string{"users"},
		},
	}

	adminUsers := []string{"admin-user"}
	adminGroups := []string{"admin-group"}

	authorizer := auth.NewAdminAuthorizer(users, adminUsers, adminGroups)

	// Test handler that will be called if authorization succeeds
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("authorized"))
	})

	handler := authorizer.Middleware(testHandler)

	tests := []struct {
		name           string
		username       string
		expectedStatus int
	}{
		{
			name:           "admin user is authorized",
			username:       "admin-user",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "user in admin group is authorized",
			username:       "group-user",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "regular user is not authorized",
			username:       "regular-user",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "no username in context is not authorized",
			username:       "",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.username != "" {
				ctx := context.WithValue(req.Context(), auth.CtxUsernameKey, tt.username)
				req = req.WithContext(ctx)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			require.Equal(t, tt.expectedStatus, rr.Code)
		})
	}
}
