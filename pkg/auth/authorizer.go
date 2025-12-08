package auth

import (
	"context"
	"fmt"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Authorizer handles MCP method authorization using Casbin RBAC
type Authorizer struct {
	enforcer *casbin.Enforcer
	users    map[string]*UserInfo
	enabled  bool // whether auth is enabled
}

// NewAuthorizer creates a new authorizer with the given users and auth rules
// If no auth rules are provided, authorization is disabled
func NewAuthorizer(users map[string]*UserInfo, authRules []AuthRule) (*Authorizer, error) {
	// If no auth rules are provided, create a disabled authorizer
	if len(authRules) == 0 {
		return &Authorizer{
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

	// Add user to group mappings with group: prefix to avoid conflicts
	for username, userInfo := range users {
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

	return &Authorizer{
		enforcer: enforcer,
		users:    users,
		enabled:  true,
	}, nil
}

// Middleware returns a mcp.Middleware that authorizes MCP method calls
func (a *Authorizer) Middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		// If auth is disabled, pass through
		if !a.enabled {
			return next(ctx, method, req)
		}

		// Extract username from context (set by Authenticator)
		username, ok := ctx.Value(CtxUsernameKey).(string)
		if !ok {
			return nil, fmt.Errorf("authentication required: username not found in context")
		}

		// Check authorization
		if err := a.authorize(ctx, method, req, username); err != nil {
			return nil, fmt.Errorf("access denied: %w", err)
		}

		return next(ctx, method, req)
	}
}

func (a *Authorizer) authorize(ctx context.Context, method string, req mcp.Request, username string) error {
	// Always allow initialization and notification methods
	switch method {
	case "initialize", "notifications/initialized":
		return nil
	}

	// Extract resource name from request params using type assertions
	resourceName := a.extractResourceName(req)

	// Check authorization using Casbin
	allowed, err := a.enforcer.Enforce("user:"+username, method, resourceName)
	if err != nil {
		return fmt.Errorf("authorization check failed: %w", err)
	}

	if !allowed {
		return fmt.Errorf("user %s is not authorized to perform %s on %s", username, method, resourceName)
	}

	return nil
}

// extractResourceName extracts the resource name from various param types
func (a *Authorizer) extractResourceName(req mcp.Request) string {
	params := req.GetParams()

	// Try to extract name from common param types
	switch p := params.(type) {
	case *mcp.CallToolParams:
		return p.Name
	case *mcp.CallToolParamsRaw:
		return p.Name
	case *mcp.GetPromptParams:
		return p.Name
	case *mcp.ReadResourceParams:
		return p.URI
	case interface{ GetName() string }:
		// Fallback for any type with a GetName method
		return p.GetName()
	case interface{ GetURI() string }:
		// Fallback for any type with a GetURI method
		return p.GetURI()
	default:
		return ""
	}
}
