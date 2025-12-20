package auth

import (
	"net/http"

	"tealpine/pkg/config"
)

// AdminAuthorizer checks if a user has admin privileges
type AdminAuthorizer struct {
	users       map[string]config.UserConfig
	adminUsers  []string
	adminGroups []string
}

// NewAdminAuthorizer creates a new admin authorizer
func NewAdminAuthorizer(users map[string]config.UserConfig, adminUsers []string, adminGroups []string) *AdminAuthorizer {
	return &AdminAuthorizer{
		users:       users,
		adminUsers:  adminUsers,
		adminGroups: adminGroups,
	}
}

// Middleware returns an HTTP middleware that checks if the authenticated user is an admin
// This should be used after the Authenticator middleware
func (a *AdminAuthorizer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get username from context (set by authenticator)
		username, ok := r.Context().Value(CtxUsernameKey).(string)
		if !ok {
			http.Error(w, "user not authenticated", http.StatusUnauthorized)
			return
		}

		// Check if user is in admin users list
		for _, adminUser := range a.adminUsers {
			if username == adminUser {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check if user belongs to any admin group
		userConfig, exists := a.users[username]
		if exists {
			for _, userGroup := range userConfig.Groups {
				for _, adminGroup := range a.adminGroups {
					if userGroup == adminGroup {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
		}

		// User is not authorized
		http.Error(w, "admin access required", http.StatusForbidden)
	})
}
