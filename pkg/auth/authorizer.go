package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Authorizer handles MCP method authorization using Casbin RBAC
type Authorizer struct {
	enforcer *casbin.Enforcer
	enabled  bool // whether auth is enabled
}

// NewAuthorizer creates a new authorizer with the given groups and auth rules.
// groups maps group name → list of usernames.
// If no auth rules are provided, authorization is disabled.
func NewAuthorizer(groups map[string][]string, authRules []AuthRule) (*Authorizer, error) {
	if len(authRules) == 0 {
		return &Authorizer{
			enabled: false,
		}, nil
	}

	m := model.NewModel()
	m.AddDef("r", "r", "sub, method, obj")
	m.AddDef("p", "p", "sub, method, obj")
	m.AddDef("g", "g", "_, _")
	m.AddDef("e", "e", "some(where (p.eft == allow))")
	m.AddDef("m", "m", "g(r.sub, p.sub) && prefixMatch(r.method, p.method) && prefixMatch(r.obj, p.obj)")

	enforcer, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin enforcer: %w", err)
	}

	enforcer.AddFunction("prefixMatch", func(args ...interface{}) (interface{}, error) {
		str, ok1 := args[0].(string)
		pattern, ok2 := args[1].(string)
		if !ok1 || !ok2 {
			return false, nil
		}
		if strings.HasSuffix(pattern, "*") {
			return strings.HasPrefix(str, strings.TrimSuffix(pattern, "*")), nil
		}
		return str == pattern, nil
	})

	// Add user-to-group mappings from groups config
	for groupName, members := range groups {
		for _, username := range members {
			_, err := enforcer.AddGroupingPolicy("user:"+username, "group:"+groupName)
			if err != nil {
				return nil, fmt.Errorf("failed to add grouping policy: %w", err)
			}
		}
	}

	// Add policies from auth rules (group-only)
	for _, rule := range authRules {
		if rule.Group == "" {
			continue
		}
		subject := "group:" + rule.Group

		method := rule.Method
		if method == "" {
			method = "*"
		}

		for _, allowPattern := range rule.Allow {
			_, err := enforcer.AddPolicy(subject, method, allowPattern)
			if err != nil {
				return nil, fmt.Errorf("failed to add policy: %w", err)
			}
		}
	}

	return &Authorizer{
		enforcer: enforcer,
		enabled:  true,
	}, nil
}

// Middleware returns a mcp.Middleware that authorizes MCP method calls
func (a *Authorizer) Middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if !a.enabled {
			return next(ctx, method, req)
		}

		username, ok := ctx.Value(CtxUsernameKey).(string)
		if !ok {
			return nil, fmt.Errorf("authentication required: username not found in context")
		}

		if err := a.authorize(ctx, method, req, username); err != nil {
			return nil, fmt.Errorf("access denied: %w", err)
		}

		return next(ctx, method, req)
	}
}

func (a *Authorizer) authorize(ctx context.Context, method string, req mcp.Request, username string) error {
	switch method {
	case "initialize", "notifications/initialized", "ping":
		return nil
	}

	resourceName := a.extractResourceName(req)

	allowed, err := a.enforcer.Enforce("user:"+username, method, resourceName)
	if err != nil {
		return fmt.Errorf("authorization check failed: %w", err)
	}

	if !allowed {
		return fmt.Errorf("user %s is not authorized to perform %s on %s", username, method, resourceName)
	}

	return nil
}

// FilteringMiddleware returns a mcp.Middleware that filters list results
func (a *Authorizer) FilteringMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, req)
		if err != nil {
			return result, err
		}

		if !a.enabled {
			return result, nil
		}

		username, ok := ctx.Value(CtxUsernameKey).(string)
		if !ok {
			return result, nil
		}

		switch method {
		case "tools/list":
			return a.filterToolsList(ctx, result, username)
		case "resources/list":
			return a.filterResourcesList(ctx, result, username)
		case "prompts/list":
			return a.filterPromptsList(ctx, result, username)
		case "resource_templates/list":
			return a.filterResourceTemplatesList(ctx, result, username)
		default:
			return result, nil
		}
	}
}

func (a *Authorizer) filterToolsList(ctx context.Context, result mcp.Result, username string) (mcp.Result, error) {
	listResult, ok := result.(*mcp.ListToolsResult)
	if !ok {
		return result, nil
	}

	filtered := make([]*mcp.Tool, 0)
	for _, tool := range listResult.Tools {
		allowed, err := a.enforcer.Enforce("user:"+username, "tools/call", tool.Name)
		if err != nil {
			continue
		}
		if allowed {
			filtered = append(filtered, tool)
		}
	}

	listResult.Tools = filtered
	return listResult, nil
}

func (a *Authorizer) filterResourcesList(ctx context.Context, result mcp.Result, username string) (mcp.Result, error) {
	listResult, ok := result.(*mcp.ListResourcesResult)
	if !ok {
		return result, nil
	}

	filtered := make([]*mcp.Resource, 0)
	for _, resource := range listResult.Resources {
		allowed, err := a.enforcer.Enforce("user:"+username, "resources/read", resource.URI)
		if err != nil {
			continue
		}
		if allowed {
			filtered = append(filtered, resource)
		}
	}

	listResult.Resources = filtered
	return listResult, nil
}

func (a *Authorizer) filterPromptsList(ctx context.Context, result mcp.Result, username string) (mcp.Result, error) {
	listResult, ok := result.(*mcp.ListPromptsResult)
	if !ok {
		return result, nil
	}

	filtered := make([]*mcp.Prompt, 0)
	for _, prompt := range listResult.Prompts {
		allowed, err := a.enforcer.Enforce("user:"+username, "prompts/get", prompt.Name)
		if err != nil {
			continue
		}
		if allowed {
			filtered = append(filtered, prompt)
		}
	}

	listResult.Prompts = filtered
	return listResult, nil
}

func (a *Authorizer) filterResourceTemplatesList(ctx context.Context, result mcp.Result, username string) (mcp.Result, error) {
	listResult, ok := result.(*mcp.ListResourceTemplatesResult)
	if !ok {
		return result, nil
	}

	filtered := make([]*mcp.ResourceTemplate, 0)
	for _, template := range listResult.ResourceTemplates {
		allowed, err := a.enforcer.Enforce("user:"+username, "resources/read", template.URITemplate)
		if err != nil {
			continue
		}
		if allowed {
			filtered = append(filtered, template)
		}
	}

	listResult.ResourceTemplates = filtered
	return listResult, nil
}

func (a *Authorizer) extractResourceName(req mcp.Request) string {
	params := req.GetParams()

	switch p := params.(type) {
	case *mcp.CallToolParams:
		return p.Name
	case *mcp.CallToolParamsRaw:
		return p.Name
	case *mcp.GetPromptParams:
		return p.Name
	case *mcp.ReadResourceParams:
		return p.URI
	case *mcp.SubscribeParams:
		return p.URI
	case *mcp.UnsubscribeParams:
		return p.URI
	case *mcp.CompleteParams:
		if p.Ref != nil {
			if p.Ref.URI != "" {
				return p.Ref.URI
			}
			return p.Ref.Name
		}
		return ""
	case interface{ GetName() string }:
		return p.GetName()
	case interface{ GetURI() string }:
		return p.GetURI()
	default:
		return ""
	}
}
