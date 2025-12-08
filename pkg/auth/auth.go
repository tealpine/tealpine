package auth

// UserInfo represents a user with their authentication token and group memberships
type UserInfo struct {
	Token  string
	Groups []string
}

// AuthRule represents an authorization rule for a user or group
type AuthRule struct {
	User   string
	Group  string
	Method string
	Allow  []string
}

const (
	CtxUsernameKey = "username"
)
