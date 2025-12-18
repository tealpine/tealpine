package auth

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
