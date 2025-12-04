package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

type Auth struct {
	enforcer   *casbin.Enforcer
	users      map[string]*UserInfo
	userTokens map[string]string // token -> username
	enabled    bool              // whether auth is enabled
}

type UserInfo struct {
	Token  string
	Groups []string
}

type AuthRule struct {
	User   string
	Group  string
	Method string
	Allow  []string
}

type MCPRequest struct {
	Method string `json:"method"`
	Params struct {
		Name string `json:"name"`
	} `json:"params"`
}

func NewAuth(users map[string]*UserInfo, authRules []AuthRule) (*Auth, error) {
	// If no auth rules are provided, create a disabled auth middleware
	if len(authRules) == 0 {
		return &Auth{
			enabled: false,
		}, nil
	}

	// Create Casbin model
	/*
		https://casbin.org/editor/

		[request_definition]
		r = sub, method, obj

		[policy_definition]
		p = sub, method, obj

		[role_definition]
		g = _, _

		[policy_effect]
		e = some(where (p.eft == allow))

		[matchers]
		m = g(r.sub, p.sub) && r.method == p.method && globMatch(r.obj, p.obj)
	*/

	m := model.NewModel()
	m.AddDef("r", "r", "sub, method, obj")
	m.AddDef("p", "p", "sub, method, obj")
	m.AddDef("g", "g", "_, _")
	m.AddDef("e", "e", "some(where (p.eft == allow))")
	m.AddDef("m", "m", "g(r.sub, p.sub) && r.method == p.method && globMatch(r.obj, p.obj)")

	// Create enforcer
	enforcer, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin enforcer: %w", err)
	}

	// Build token map
	userTokens := make(map[string]string)
	for username, userInfo := range users {
		userTokens[userInfo.Token] = username

		// Add user to group mappings with group: prefix to avoid conflicts
		for _, group := range userInfo.Groups {
			_, err := enforcer.AddGroupingPolicy("user:"+username, "group:"+group)
			if err != nil {
				return nil, fmt.Errorf("failed to add grouping policy: %w", err)
			}
		}
	}

	// Add policies from auth rules
	for _, rule := range authRules {
		var subject string
		if rule.User != "" {
			subject = "user:" + rule.User
		} else if rule.Group != "" {
			subject = "group:" + rule.Group
		}
		if subject == "" {
			continue
		}

		method := rule.Method
		if method == "" {
			// If no method specified, apply to all methods
			method = "*"
		}

		// Add policy for each allowed pattern
		for _, allowPattern := range rule.Allow {
			_, err := enforcer.AddPolicy(subject, method, allowPattern)
			if err != nil {
				return nil, fmt.Errorf("failed to add policy: %w", err)
			}
		}
	}

	return &Auth{
		enforcer:   enforcer,
		users:      users,
		userTokens: userTokens,
		enabled:    true,
	}, nil
}

func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If auth is disabled, pass through
		if !a.enabled {
			next.ServeHTTP(w, r)
			return
		}

		username, ok := a.authenticateUser(w, r)
		if !ok {
			return
		}

		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// If the body is empty, it might be a ping/health check - pass it through
		if len(body) == 0 {
			r.Body = io.NopCloser(bytes.NewBuffer(body))
			next.ServeHTTP(w, r)
			return
		}

		// Unmarshal the MCP request to extract method and name
		var mcpReq MCPRequest
		if err := json.Unmarshal(body, &mcpReq); err != nil {
			http.Error(w, "failed to parse MCP request", http.StatusBadRequest)
			return
		}

		if ok := a.authorize(mcpReq, username, w); !ok {
			return
		}

		// Restore the request body for the next handler
		r.Body = io.NopCloser(bytes.NewBuffer(body))

		next.ServeHTTP(w, r)
	})
}

func (a *Auth) authenticateUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	// Extract bearer token from the Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "missing authorization header", http.StatusUnauthorized)
		return "", false
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
		return "", false
	}

	// Find user by token
	username, ok := a.userTokens[token]
	if !ok {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return "", false
	}
	return username, true
}

func (a *Auth) authorize(mcpReq MCPRequest, username string, w http.ResponseWriter) bool {
	allowed := false
	switch mcpReq.Method {
	case "initialize", "notifications/initialized":
		allowed = true
	default:
		var err error
		allowed, err = a.enforcer.Enforce("user:"+username, mcpReq.Method, mcpReq.Params.Name)
		if err != nil {
			http.Error(w, fmt.Sprintf("authorization check failed: %v", err), http.StatusInternalServerError)
			return false
		}
	}

	if !allowed {
		http.Error(w, "access denied", http.StatusForbidden)
		return false
	}
	return true
}
