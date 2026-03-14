package auth

import (
	"net/http"
)

// AdminAuthorizer checks if a user has admin privileges based on group membership
type AdminAuthorizer struct {
	groups      map[string][]string
	adminGroups []string
}

// NewAdminAuthorizer creates a new admin authorizer.
// groups maps group name → list of usernames.
// adminGroups is the list of group names that have admin access.
func NewAdminAuthorizer(groups map[string][]string, adminGroups []string) *AdminAuthorizer {
	return &AdminAuthorizer{
		groups:      groups,
		adminGroups: adminGroups,
	}
}

// Middleware returns an HTTP middleware that checks if the authenticated user is an admin
func (a *AdminAuthorizer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, ok := r.Context().Value(CtxUsernameKey).(string)
		if !ok {
			http.Error(w, "user not authenticated", http.StatusUnauthorized)
			return
		}

		for _, adminGroup := range a.adminGroups {
			for _, member := range a.groups[adminGroup] {
				if member == username {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		http.Error(w, "admin access required", http.StatusForbidden)
	})
}
