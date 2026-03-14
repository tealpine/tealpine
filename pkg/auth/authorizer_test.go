package auth

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestNewAuthorizer_NoRules(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice"},
		"gr2": {"alice"},
	}

	authorizer, err := NewAuthorizer(groups, []AuthRule{})
	require.NoError(t, err)
	require.NotNil(t, authorizer)
	require.False(t, authorizer.enabled, "Authorizer should be disabled when no rules provided")
}

func TestNewAuthorizer_WithRules(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice", "bob"},
		"gr2": {"alice"},
		"gr3": {"bob"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/list", Allow: []string{"temp_*"}},
		{Group: "gr2", Method: "tools/call", Allow: []string{"calc_calculate"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)
	require.NotNil(t, authorizer)
	require.True(t, authorizer.enabled, "Authorizer should be enabled when rules provided")
}

func TestAuthorizerMiddleware_Disabled(t *testing.T) {
	authorizer, err := NewAuthorizer(make(map[string][]string), []AuthRule{})
	require.NoError(t, err)

	called := false
	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}

	ctx := context.Background()

	middleware := authorizer.Middleware(handler)
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "test"},
	}
	_, err = middleware(ctx, "tools/list", req)

	require.True(t, called, "Handler should be called")
	require.NoError(t, err)
}

func TestAuthorizerMiddleware_MissingUsername(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/list", Allow: []string{"*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

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
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/list", Allow: []string{"temp_*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
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
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/list", Allow: []string{"temp_*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
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

func TestAuthorizerMiddleware_PingMethod_Allowed(t *testing.T) {
	groups := map[string][]string{
		"gr-alice": {"alice"},
		"gr-bob":   {"bob"},
	}

	authRules := []AuthRule{
		{Group: "gr-alice", Method: "tools/call", Allow: []string{"temp_*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	// Both alice and bob should be allowed to call ping
	for _, username := range []string{"alice", "bob"} {
		t.Run(username, func(t *testing.T) {
			called := false
			h := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
				called = true
				return nil, nil
			}
			mw := authorizer.Middleware(h)
			ctx := context.WithValue(context.Background(), CtxUsernameKey, username)
			req := &mcp.ServerRequest[*mcp.PingParams]{
				Params: &mcp.PingParams{},
			}
			_, err := mw(ctx, "ping", req)
			require.NoError(t, err, "ping should be allowed for %s", username)
			require.True(t, called, "Handler should be called for %s", username)
		})
	}

	// Verify it's not a blanket bypass — bob should still be denied tools/call
	middleware := authorizer.Middleware(func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	})
	ctx := context.WithValue(context.Background(), CtxUsernameKey, "bob")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "temp_get"},
	}
	_, err = middleware(ctx, "tools/call", req)
	require.Error(t, err, "bob should still be denied tools/call")
}

func TestAuthorizerMiddleware_GroupPermission_Allowed(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice"},
		"gr2": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr2", Method: "tools/call", Allow: []string{"temp_*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	called := false
	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "temp_get_room_temperature"},
	}

	middleware := authorizer.Middleware(handler)
	_, err = middleware(ctx, "tools/call", req)

	require.NoError(t, err)
	require.True(t, called, "Handler should be called for alice (member of gr2)")
}

func TestAuthorizerMiddleware_GroupPermission_Denied(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice", "bob"},
		"gr2": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr2", Method: "tools/call", Allow: []string{"calc_calculate"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "bob")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "calc_calculate"},
	}

	middleware := authorizer.Middleware(handler)
	_, err = middleware(ctx, "tools/call", req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "access denied")
}

func TestAuthorizerMiddleware_WildcardPattern(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/call", Allow: []string{"temp_*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
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
				Params: &mcp.CallToolParams{Name: tc.toolName},
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
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/list", Allow: []string{"calculate"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "calculate"},
	}

	middleware := authorizer.Middleware(handler)

	_, err = middleware(ctx, "tools/list", req)
	require.NoError(t, err, "Should allow tools/list")

	_, err = middleware(ctx, "tools/call", req)
	require.Error(t, err, "Should deny tools/call")
	require.Contains(t, err.Error(), "access denied")
}

func TestAuthorizerMiddleware_NoResourceName(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "some/method", Allow: []string{"*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: ""},
	}

	middleware := authorizer.Middleware(handler)
	_, err = middleware(ctx, "some/method", req)

	require.NoError(t, err, "Should allow when resource name matches wildcard")
}

func TestAuthorizerMiddleware_WildcardMethod(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "*", Allow: []string{"*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	called := false
	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")

	methods := []string{
		"tools/call",
		"tools/list",
		"resources/read",
		"resources/list",
		"prompts/get",
		"prompts/list",
		"logging/setLevel",
		"some/custom/method",
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			called = false
			req := &mcp.ServerRequest[*mcp.CallToolParams]{
				Params: &mcp.CallToolParams{Name: "anything"},
			}
			middleware := authorizer.Middleware(handler)
			_, err := middleware(ctx, method, req)
			require.NoError(t, err, "Wildcard method should allow %s", method)
			require.True(t, called, "Handler should be called for %s", method)
		})
	}
}

func TestAuthorizerMiddleware_PrefixMethod(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/*", Allow: []string{"*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	middleware := authorizer.Middleware(handler)

	tests := []struct {
		name    string
		method  string
		allowed bool
	}{
		{"tools/call", "tools/call", true},
		{"tools/list", "tools/list", true},
		{"resources/read", "resources/read", false},
		{"prompts/get", "prompts/get", false},
		{"logging/setLevel", "logging/setLevel", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &mcp.ServerRequest[*mcp.CallToolParams]{
				Params: &mcp.CallToolParams{Name: "anything"},
			}
			_, err := middleware(ctx, tt.method, req)
			if tt.allowed {
				require.NoError(t, err, "method %q should be allowed by pattern 'tools/*'", tt.method)
			} else {
				require.Error(t, err, "method %q should be denied by pattern 'tools/*'", tt.method)
			}
		})
	}
}

func TestAuthorizerMiddleware_WildcardMethod_DeniedForOtherUser(t *testing.T) {
	groups := map[string][]string{
		"gr-alice": {"alice"},
		"gr-bob":   {"bob"},
	}

	authRules := []AuthRule{
		{Group: "gr-alice", Method: "*", Allow: []string{"*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "bob")
	req := &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{Name: "anything"},
	}

	middleware := authorizer.Middleware(handler)
	_, err = middleware(ctx, "logging/setLevel", req)
	require.Error(t, err, "Bob should be denied with wildcard method rule for alice's group only")
	require.Contains(t, err.Error(), "access denied")
}

func TestAuthorizerMiddleware_WildcardObj(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/call", Allow: []string{"*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	middleware := authorizer.Middleware(handler)

	tests := []struct {
		name string
		obj  string
	}{
		{"simple_name", "mytool"},
		{"hyphenated", "my-tool"},
		{"underscored", "my_tool"},
		{"uri_with_slashes", "file:///home/user/doc.txt"},
		{"path_like", "some/nested/resource"},
		{"url", "https://example.com/resource"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &mcp.ServerRequest[*mcp.CallToolParams]{
				Params: &mcp.CallToolParams{Name: tt.obj},
			}
			_, err := middleware(ctx, "tools/call", req)
			require.NoError(t, err, "allow pattern '*' should match obj %q", tt.obj)
		})
	}
}

func TestAuthorizerMiddleware_GlobPatternObj(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "resources/read", Allow: []string{"file://*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	handler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return nil, nil
	}

	ctx := context.WithValue(context.Background(), CtxUsernameKey, "alice")
	middleware := authorizer.Middleware(handler)

	tests := []struct {
		name    string
		uri     string
		allowed bool
	}{
		{"file_shallow", "file://readme.txt", true},
		{"file_deep_path", "file:///home/user/doc.txt", true},
		{"https_blocked", "https://example.com/resource", false},
		{"no_scheme", "readme.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &mcp.ServerRequest[*mcp.ReadResourceParams]{
				Params: &mcp.ReadResourceParams{URI: tt.uri},
			}
			_, err := middleware(ctx, "resources/read", req)
			if tt.allowed {
				require.NoError(t, err, "pattern 'file://*' should match %q", tt.uri)
			} else {
				require.Error(t, err, "pattern 'file://*' should NOT match %q", tt.uri)
			}
		})
	}
}

func TestFilteringMiddleware_ToolsList_FilterByPermission(t *testing.T) {
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/call", Allow: []string{"temp_*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

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
	groups := map[string][]string{
		"gr1": {"alice"},
		"gr2": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/call", Allow: []string{"temp_*"}},
		{Group: "gr2", Method: "tools/call", Allow: []string{"calc_*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
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
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/call", Allow: []string{"nonexistent_*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
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
	authorizer, err := NewAuthorizer(make(map[string][]string), []AuthRule{})
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
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "resources/read", Allow: []string{"file://docs/*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
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
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "prompts/get", Allow: []string{"code_*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
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
	groups := map[string][]string{
		"gr1": {"alice"},
	}

	authRules := []AuthRule{
		{Group: "gr1", Method: "tools/call", Allow: []string{"temp_*"}},
	}

	authorizer, err := NewAuthorizer(groups, authRules)
	require.NoError(t, err)

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
