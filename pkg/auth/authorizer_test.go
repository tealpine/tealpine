package auth

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestNewAuthorizer_NoRules(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
	}

	authorizer, err := NewAuthorizer(users, []AuthRule{})
	require.NoError(t, err)
	require.NotNil(t, authorizer)
	require.False(t, authorizer.enabled, "Authorizer should be disabled when no rules provided")
}

func TestNewAuthorizer_WithRules(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
		"bob": {
			Token:  "bobtoken",
			Groups: []string{"gr1", "gr3"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"temp_*"},
		},
		{
			Group:  "gr2",
			Method: "tools/call",
			Allow:  []string{"calc_calculate"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)
	require.NotNil(t, authorizer)
	require.True(t, authorizer.enabled, "Authorizer should be enabled when rules provided")
}

func TestAuthorizerMiddleware_Disabled(t *testing.T) {
	// Create authorizer with no rules (disabled)
	authorizer, err := NewAuthorizer(make(map[string]*UserInfo), []AuthRule{})
	require.NoError(t, err)

	// Create test handler
	called := false
	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}

	// Create context without username (simulating missing authentication)
	ctx := context.Background()

	// Call middleware
	middleware := authorizer.Middleware(handler)
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "test"},
	}
	_, err = middleware(ctx, "tools/list", req)

	// Should pass through without auth check
	require.True(t, called, "Handler should be called")
	require.NoError(t, err)
}

func TestAuthorizerMiddleware_MissingUsername(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	// Create context without username
	ctx := context.Background()

	middleware := authorizer.Middleware(handler)
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "test"},
	}
	_, err = middleware(ctx, "tools/list", req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "username not found in context")
}

func TestAuthorizerMiddleware_InitializeMethod_Allowed(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"temp_*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	called := false
	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")

	middleware := authorizer.Middleware(handler)
	req := &mcp.ServerRequest[*mcp.InitializeParams]{
		Params: &mcp.InitializeParams{},
	}
	_, err = middleware(ctx, "initialize", req)

	require.NoError(t, err)
	require.True(t, called, "Handler should be called for initialize method")
}

func TestAuthorizerMiddleware_NotificationsInitialized_Allowed(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"temp_*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	called := false
	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")

	middleware := authorizer.Middleware(handler)
	req := &mcp.ServerRequest[*mcp.InitializedParams]{
		Params: &mcp.InitializedParams{},
	}
	_, err = middleware(ctx, "notifications/initialized", req)

	require.NoError(t, err)
	require.True(t, called, "Handler should be called for notifications/initialized method")
}

func TestAuthorizerMiddleware_UserDirectPermission_Allowed(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/call",
			Allow:  []string{"temp_*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	called := false
	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{
			Name: "temp_get_room_temperature",
		},
	}

	middleware := authorizer.Middleware(handler)
	_, err = middleware(ctx, "tools/call", req)

	require.NoError(t, err)
	require.True(t, called, "Handler should be called for allowed tool")
}

func TestAuthorizerMiddleware_UserDirectPermission_Denied(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/call",
			Allow:  []string{"temp_*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{
			Name: "calc_calculate",
		},
	}

	middleware := authorizer.Middleware(handler)
	_, err = middleware(ctx, "tools/call", req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "access denied")
}

func TestAuthorizerMiddleware_GroupPermission_Allowed(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
		"bob": {
			Token:  "bobtoken",
			Groups: []string{"gr1", "gr3"},
		},
	}

	authRules := []AuthRule{
		{
			Group:  "gr2",
			Method: "tools/call",
			Allow:  []string{"calc_calculate"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	called := false
	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{
			Name: "calc_calculate",
		},
	}

	middleware := authorizer.Middleware(handler)
	_, err = middleware(ctx, "tools/call", req)

	require.NoError(t, err)
	require.True(t, called, "Handler should be called for alice (member of gr2)")
}

func TestAuthorizerMiddleware_GroupPermission_Denied(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
		"bob": {
			Token:  "bobtoken",
			Groups: []string{"gr1", "gr3"},
		},
	}

	authRules := []AuthRule{
		{
			Group:  "gr2",
			Method: "tools/call",
			Allow:  []string{"calc_calculate"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "bob")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{
			Name: "calc_calculate",
		},
	}

	middleware := authorizer.Middleware(handler)
	_, err = middleware(ctx, "tools/call", req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "access denied")
}

func TestAuthorizerMiddleware_WildcardPattern(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/call",
			Allow:  []string{"temp_*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	testCases := []struct {
		name     string
		toolName string
		allowed  bool
	}{
		{"temp_get_room_temperature", "temp_get_room_temperature", true},
		{"temp_get_all_temperatures", "temp_get_all_temperatures", true},
		{"temp_any", "temp_any", true},
		{"calc_calculate", "calc_calculate", false},
		{"hello_world", "hello_world", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
			req := &mcp.ServerRequest[*mcp.CallToolParams]{
				Params: &mcp.CallToolParams{
					Name: tc.toolName,
				},
			}

			middleware := authorizer.Middleware(handler)
			_, err := middleware(ctx, "tools/call", req)

			if tc.allowed {
				require.NoError(t, err, "Should allow %s", tc.toolName)
			} else {
				require.Error(t, err, "Should deny %s", tc.toolName)
				require.Contains(t, err.Error(), "access denied")
			}
		})
	}
}

func TestAuthorizerMiddleware_MethodSpecific(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"calculate"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{
			Name: "calculate",
		},
	}

	middleware := authorizer.Middleware(handler)

	// Test with allowed method
	_, err = middleware(ctx, "tools/list", req)
	require.NoError(t, err, "Should allow tools/list")

	// Test with different method (same tool)
	_, err = middleware(ctx, "tools/call", req)
	require.Error(t, err, "Should deny tools/call")
	require.Contains(t, err.Error(), "access denied")
}

func TestAuthorizerMiddleware_NoResourceName(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "some/method",
			Allow:  []string{"*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	// Use a params type that has no name field - just use an empty CallToolParams
	// which will have an empty Name that matches the wildcard "*"
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{
			Name: "",
		},
	}

	middleware := authorizer.Middleware(handler)
	_, err = middleware(ctx, "some/method", req)

	require.NoError(t, err, "Should allow when resource name matches wildcard")
}

func TestFilteringMiddleware_ToolsList_FilterByPermission(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/call",
			Allow:  []string{"temp_*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	// Create handler that returns a list of tools
	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return &mcp.ListToolsResult{
			Tools: []*mcp.Tool{
				{Name: "temp_get_room_temperature"},
				{Name: "temp_get_all_temperatures"},
				{Name: "calc_calculate"},
				{Name: "hello_world"},
			},
		}, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.ListToolsParams]{
		Params: &mcp.ListToolsParams{},
	}

	middleware := authorizer.FilteringMiddleware(handler)
	result, err := middleware(ctx, "tools/list", req)

	require.NoError(t, err)
	listResult, ok := result.(*mcp.ListToolsResult)
	require.True(t, ok, "Result should be ListToolsResult")
	require.Len(t, listResult.Tools, 2, "Should only include temp_* tools")
	require.Equal(t, "temp_get_room_temperature", listResult.Tools[0].Name)
	require.Equal(t, "temp_get_all_temperatures", listResult.Tools[1].Name)
}

func TestFilteringMiddleware_ToolsList_MultiplePermissions(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1", "gr2"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/call",
			Allow:  []string{"temp_*"},
		},
		{
			Group:  "gr2",
			Method: "tools/call",
			Allow:  []string{"calc_*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return &mcp.ListToolsResult{
			Tools: []*mcp.Tool{
				{Name: "temp_get_room_temperature"},
				{Name: "calc_calculate"},
				{Name: "hello_world"},
			},
		}, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.ListToolsParams]{
		Params: &mcp.ListToolsParams{},
	}

	middleware := authorizer.FilteringMiddleware(handler)
	result, err := middleware(ctx, "tools/list", req)

	require.NoError(t, err)
	listResult, ok := result.(*mcp.ListToolsResult)
	require.True(t, ok)
	require.Len(t, listResult.Tools, 2, "Should include both temp_* and calc_* tools")
}

func TestFilteringMiddleware_ToolsList_NoPermissions(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/call",
			Allow:  []string{"nonexistent_*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return &mcp.ListToolsResult{
			Tools: []*mcp.Tool{
				{Name: "temp_get_room_temperature"},
				{Name: "calc_calculate"},
			},
		}, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.ListToolsParams]{
		Params: &mcp.ListToolsParams{},
	}

	middleware := authorizer.FilteringMiddleware(handler)
	result, err := middleware(ctx, "tools/list", req)

	require.NoError(t, err)
	listResult, ok := result.(*mcp.ListToolsResult)
	require.True(t, ok)
	require.Len(t, listResult.Tools, 0, "Should return empty list when no permissions match")
}

func TestFilteringMiddleware_ToolsList_Disabled(t *testing.T) {
	// Create authorizer with no rules (disabled)
	authorizer, err := NewAuthorizer(make(map[string]*UserInfo), []AuthRule{})
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return &mcp.ListToolsResult{
			Tools: []*mcp.Tool{
				{Name: "tool1"},
				{Name: "tool2"},
			},
		}, nil
	}

	ctx := context.Background()
	req := &mcp.ServerRequest[*mcp.ListToolsParams]{
		Params: &mcp.ListToolsParams{},
	}

	middleware := authorizer.FilteringMiddleware(handler)
	result, err := middleware(ctx, "tools/list", req)

	require.NoError(t, err)
	listResult, ok := result.(*mcp.ListToolsResult)
	require.True(t, ok)
	require.Len(t, listResult.Tools, 2, "Should not filter when auth is disabled")
}

func TestFilteringMiddleware_ResourcesList(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "resources/read",
			Allow:  []string{"file://docs/*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return &mcp.ListResourcesResult{
			Resources: []*mcp.Resource{
				{URI: "file://docs/readme.md"},
				{URI: "file://config/settings.json"},
			},
		}, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.ListResourcesParams]{
		Params: &mcp.ListResourcesParams{},
	}

	middleware := authorizer.FilteringMiddleware(handler)
	result, err := middleware(ctx, "resources/list", req)

	require.NoError(t, err)
	listResult, ok := result.(*mcp.ListResourcesResult)
	require.True(t, ok)
	require.Len(t, listResult.Resources, 1, "Should only include file://docs/* resources")
	require.Equal(t, "file://docs/readme.md", listResult.Resources[0].URI)
}

func TestFilteringMiddleware_PromptsList(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "prompts/get",
			Allow:  []string{"code_*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return &mcp.ListPromptsResult{
			Prompts: []*mcp.Prompt{
				{Name: "code_review"},
				{Name: "code_generate"},
				{Name: "doc_write"},
			},
		}, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.ListPromptsParams]{
		Params: &mcp.ListPromptsParams{},
	}

	middleware := authorizer.FilteringMiddleware(handler)
	result, err := middleware(ctx, "prompts/list", req)

	require.NoError(t, err)
	listResult, ok := result.(*mcp.ListPromptsResult)
	require.True(t, ok)
	require.Len(t, listResult.Prompts, 2, "Should only include code_* prompts")
	require.Equal(t, "code_review", listResult.Prompts[0].Name)
	require.Equal(t, "code_generate", listResult.Prompts[1].Name)
}

func TestFilteringMiddleware_NonListMethod_PassThrough(t *testing.T) {
	users := map[string]*UserInfo{
		"alice": {
			Token:  "alicetoken",
			Groups: []string{"gr1"},
		},
	}

	authRules := []AuthRule{
		{
			User:   "alice",
			Method: "tools/call",
			Allow:  []string{"temp_*"},
		},
	}

	authorizer, err := NewAuthorizer(users, authRules)
	require.NoError(t, err)

	// Return a CallToolResult (not a list result)
	expectedResult := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "test result"},
		},
	}

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return expectedResult, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "temp_tool"},
	}

	middleware := authorizer.FilteringMiddleware(handler)
	result, err := middleware(ctx, "tools/call", req)

	require.NoError(t, err)
	require.Equal(t, expectedResult, result, "Should pass through non-list results unchanged")
}
