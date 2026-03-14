package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"tealpine/pkg/auth"
)

func TestAdminAuthorizer(t *testing.T) {
	groups := map[string][]string{
		"admin-group": {"group-user"},
		"users":       {"regular-user"},
	}

	adminGroups := []string{"admin-group"}

	authorizer := auth.NewAdminAuthorizer(groups, adminGroups)

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
