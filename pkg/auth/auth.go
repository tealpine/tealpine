package auth

// AuthRule represents an authorization rule for a group
type AuthRule struct {
	Group  string
	Method string
	Allow  []string
}

const (
	CtxUsernameKey = "username"
)
