package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"tealpine/pkg/auth"
	"tealpine/pkg/client"
	"tealpine/pkg/config"
	mcptest "tealpine/test"
)

func TestProxyStreamableHttpCalculator(t *testing.T) {
	// Create the MCP calculator server
	calcServer := &mcptest.MCPCalculator{}
	handler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err, "Failed to get HTTP handler")

	// Create httptest server for upstream MCP
	upstreamServer := httptest.NewServer(handler)
	defer upstreamServer.Close()

	// Create upstream client configuration
	upstreamConfig := config.UpstreamConfig{
		Name:      "calculator",
		Transport: "streamablehttp",
		URL:       upstreamServer.URL + "/mcp",
	}

	// Initialize upstream client
	upstreamClient := client.NewClient(upstreamConfig)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	upstreamClient.Start(ctx)

	err = upstreamClient.WaitForConnection(ctx)
	require.NoError(t, err)
	defer upstreamClient.Close()

	// Create authenticator and authorizer (no auth for test)
	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig), nil, "")
	authorizer, err := auth.NewAuthorizer(make(map[string][]string), []auth.AuthRule{})
	require.NoError(t, err)

	// Create proxy
	proxy, err := NewSingleProxy("streamablehttp", upstreamClient, "", authenticator, authorizer)
	require.NoError(t, err)
	err = proxy.Init(ctx)
	require.NoError(t, err)

	// Create httptest server for proxy
	httpServer := httptest.NewServer(proxy)
	defer httpServer.Close()

	// Create proxy client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-proxy-client",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL + "/mcp",
	}

	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err)
	defer session.Close()

	// Test tool call
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "calculate",
		Arguments: map[string]interface{}{
			"operation": "add",
			"x":         20,
			"y":         22,
		},
	})

	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "42.00", result.Content[0].(*mcp.TextContent).Text)
}

func TestProxyStreamableHttpTemperature(t *testing.T) {
	// Create the MCP temperature server
	tempServer := &mcptest.MCPTemperature{}
	handler, err := tempServer.GetHTTPHandler()
	require.NoError(t, err, "Failed to get HTTP handler")

	// Create httptest server for upstream MCP
	upstreamServer := httptest.NewServer(handler)
	defer upstreamServer.Close()

	// Create upstream client configuration
	upstreamConfig := config.UpstreamConfig{
		Name:      "temperature",
		Transport: "streamablehttp",
		URL:       upstreamServer.URL + "/mcp",
	}

	// Initialize upstream client
	upstreamClient := client.NewClient(upstreamConfig)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	upstreamClient.Start(ctx)

	err = upstreamClient.WaitForConnection(ctx)
	require.NoError(t, err)
	defer upstreamClient.Close()

	// Create authenticator and authorizer (no auth for test)
	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig), nil, "")
	authorizer, err := auth.NewAuthorizer(make(map[string][]string), []auth.AuthRule{})
	require.NoError(t, err)

	// Create proxy
	proxy, err := NewSingleProxy("streamablehttp", upstreamClient, "", authenticator, authorizer)
	require.NoError(t, err)
	err = proxy.Init(ctx)
	require.NoError(t, err)

	// Create httptest server for proxy
	httpServer := httptest.NewServer(proxy)
	defer httpServer.Close()

	// Create proxy client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-temp-proxy-client",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL + "/mcp",
	}

	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err)
	defer session.Close()

	// Test tool call
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_room_temperature",
		Arguments: map[string]interface{}{
			"room": "bedroom",
		},
	})

	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Contains(t, result.Content[0].(*mcp.TextContent).Text, "The temperature in the bedroom is")
}

func TestMultiProxy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Create upstream MCP servers
	// Calculator server
	calcServer := &mcptest.MCPCalculator{}
	calcHandler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err)
	calcUpstreamServer := httptest.NewServer(calcHandler)
	defer calcUpstreamServer.Close()

	// Temperature server
	tempServer := &mcptest.MCPTemperature{}
	tempHandler, err := tempServer.GetHTTPHandler()
	require.NoError(t, err)
	tempUpstreamServer := httptest.NewServer(tempHandler)
	defer tempUpstreamServer.Close()

	// Hello server (stdio)
	// Note: We'll use stdio for hello server as in the original test

	// 2. Create and initialize clients
	clients := make(map[string]*client.Client)

	// Calculator client
	calcConfig := config.UpstreamConfig{
		Name:      "calculator",
		Transport: "streamablehttp",
		URL:       calcUpstreamServer.URL + "/mcp",
	}
	calcClient := client.NewClient(calcConfig)
	calcClient.Start(ctx)

	err = calcClient.WaitForConnection(ctx)
	require.NoError(t, err)
	clients["calculator"] = calcClient
	defer calcClient.Close()

	// Temperature client
	tempConfig := config.UpstreamConfig{
		Name:      "temperature",
		Transport: "streamablehttp",
		URL:       tempUpstreamServer.URL + "/mcp",
	}
	tempClient := client.NewClient(tempConfig)
	tempClient.Start(ctx)
	err = tempClient.WaitForConnection(ctx)
	require.NoError(t, err)
	clients["temperature"] = tempClient
	defer tempClient.Close()

	// Hello client (stdio)
	helloConfig := config.UpstreamConfig{
		Name:      "hello",
		Transport: "stdio",
		Cmd:       "go",
		CmdArgs: []string{
			"run",
			"../../test/mcp_servers/main.go",
			"server",
			"hello",
		},
	}
	helloClient := client.NewClient(helloConfig)
	helloClient.Start(ctx)
	err = helloClient.WaitForConnection(ctx)
	require.NoError(t, err)
	clients["hello"] = helloClient
	defer helloClient.Close()

	// 3. Create multi-proxy config
	multiProxyConfig := []config.MultiUpstreamConfig{
		{Name: "calculator", Prefix: "calc"},
		{Name: "temperature", Prefix: "temp"},
		{Name: "hello", Prefix: "hello"},
	}

	// 4. Create authenticator and authorizer (no auth for test)
	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig), nil, "")
	authorizer, err := auth.NewAuthorizer(make(map[string][]string), []auth.AuthRule{})
	require.NoError(t, err)

	// 5. Create MultiProxy
	proxy, err := NewMultiProxy("streamablehttp", clients, multiProxyConfig, "", authenticator, authorizer)
	require.NoError(t, err)
	err = proxy.Init(ctx)
	require.NoError(t, err)

	// 6. Start test server
	httpServer := httptest.NewServer(proxy)
	defer httpServer.Close()

	// 7. Create client for the proxy
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-multi-proxy-client",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL + "/mcp",
	}

	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err)
	defer session.Close()

	// 8. Call tools
	// Calculator
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "calc_calculate", // Prefixed name
		Arguments: map[string]interface{}{
			"operation": "multiply",
			"x":         6,
			"y":         7,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "42.00", result.Content[0].(*mcp.TextContent).Text)

	// Temperature
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "temp_get_room_temperature", // Prefixed name
		Arguments: map[string]interface{}{
			"room": "living room",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Contains(t, result.Content[0].(*mcp.TextContent).Text, "The temperature in the living room is")

	// Hello
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "hello_hello_world", // Prefixed name
		Arguments: map[string]interface{}{
			"name": "Multi-Proxy",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "Hello, Multi-Proxy!", result.Content[0].(*mcp.TextContent).Text)
}

func TestProxyFilteringMiddleware_ToolsList(t *testing.T) {
	// Create the MCP calculator server (has "calculate" tool)
	calcServer := &mcptest.MCPCalculator{}
	handler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err, "Failed to get HTTP handler")

	// Create httptest server for upstream MCP
	upstreamServer := httptest.NewServer(handler)
	defer upstreamServer.Close()

	// Create upstream client configuration
	upstreamConfig := config.UpstreamConfig{
		Name:      "calculator",
		Transport: "streamablehttp",
		URL:       upstreamServer.URL + "/mcp",
	}

	// Initialize upstream client
	upstreamClient := client.NewClient(upstreamConfig)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	upstreamClient.Start(ctx)
	err = upstreamClient.WaitForConnection(ctx)
	require.NoError(t, err)
	defer upstreamClient.Close()

	// Create users and auth rules
	// Alice can only list/call "calculate" tool (which the calculator server provides)
	// Bob has no permissions
	users := map[string]config.UserConfig{
		"alice": {
			Token: "alice-token",
		},
		"bob": {
			Token: "bob-token",
		},
	}

	groups := map[string][]string{
		"gr-alice": {"alice"},
		"gr-bob":   {"bob"},
	}

	authRules := []auth.AuthRule{
		{
			Group:  "gr-alice",
			Method: "tools/list",
			Allow:  []string{"*"}, // Allow listing all tools
		},
		{
			Group:  "gr-alice",
			Method: "tools/call",
			Allow:  []string{"calculate"}, // But only allow calling "calculate"
		},
		{
			Group:  "gr-bob",
			Method: "tools/list",
			Allow:  []string{"*"}, // Allow listing
		},
		{
			Group:  "gr-bob",
			Method: "tools/call",
			Allow:  []string{"nonexistent_*"}, // But no actual tools
		},
	}

	authenticator := auth.NewAuthenticator(users, nil, "")
	authorizer, err := auth.NewAuthorizer(groups, authRules)
	require.NoError(t, err)

	// Create single proxy
	proxy, err := NewSingleProxy("streamablehttp", upstreamClient, "calc", authenticator, authorizer)
	require.NoError(t, err)

	err = proxy.Init(ctx)
	require.NoError(t, err)

	// Create httptest server for the proxy
	httpServer := httptest.NewServer(proxy)
	defer httpServer.Close()

	// Test with Alice's token
	t.Run("Alice_CanSeeCalculateTool", func(t *testing.T) {
		aliceClient := mcp.NewClient(&mcp.Implementation{
			Name:    "alice-client",
			Version: "1.0.0",
		}, nil)

		transport := &mcp.StreamableClientTransport{
			Endpoint: httpServer.URL + "/mcp",
			HTTPClient: &http.Client{
				Transport: &testBearerAuthTransport{
					token:   "alice-token",
					wrapped: http.DefaultTransport,
				},
			},
		}

		session, err := aliceClient.Connect(ctx, transport, nil)
		require.NoError(t, err)
		defer session.Close()

		// List tools - should see "calculate"
		toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		require.NoError(t, err)
		require.Len(t, toolsResult.Tools, 1, "Alice should see 1 tool")
		require.Equal(t, "calculate", toolsResult.Tools[0].Name)

		// Call calculate tool - should succeed
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "add",
				"x":         5,
				"y":         3,
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "8.00", result.Content[0].(*mcp.TextContent).Text)
	})

	// Test with Bob's token
	t.Run("Bob_CannotSeeAnyTools", func(t *testing.T) {
		bobClient := mcp.NewClient(&mcp.Implementation{
			Name:    "bob-client",
			Version: "1.0.0",
		}, nil)

		transport := &mcp.StreamableClientTransport{
			Endpoint: httpServer.URL + "/mcp",
			HTTPClient: &http.Client{
				Transport: &testBearerAuthTransport{
					token:   "bob-token",
					wrapped: http.DefaultTransport,
				},
			},
		}

		session, err := bobClient.Connect(ctx, transport, nil)
		require.NoError(t, err)
		defer session.Close()

		// List tools - should see empty list (filtered out)
		toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		require.NoError(t, err)
		require.Len(t, toolsResult.Tools, 0, "Bob should see 0 tools (filtered)")

		// Try to call calculate tool - should fail authorization
		_, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "add",
				"x":         5,
				"y":         3,
			},
		})
		require.Error(t, err, "Bob should not be able to call calculate")
	})
}

// testBearerAuthTransport is a custom HTTP transport that adds Bearer token authentication
type testBearerAuthTransport struct {
	token   string
	wrapped http.RoundTripper
}

func (t *testBearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.wrapped.RoundTrip(req)
}

// --- naming strategy unit tests ---

func TestMultiProxyNamingStrategy_TransformURI(t *testing.T) {
	m := &multiProxyNamingStrategy{prefix: "p"}
	// URI with scheme: insert prefix after "://"
	require.Equal(t, "res://p/full/note", m.TransformURI("res://full/note"))
	require.Equal(t, "foo://p/bar", m.TransformURI("foo://bar"))
	// URI without scheme: prefix_name
	require.Equal(t, "p_name", m.TransformURI("name"))
	require.Equal(t, "p_", m.TransformURI(""))
	// TransformName always prefixes
	require.Equal(t, "p_echo", m.TransformName("echo"))
}

func TestSingleProxyNamingStrategy_IsIdentity(t *testing.T) {
	s := &singleProxyNamingStrategy{}
	require.Equal(t, "my_tool", s.TransformName("my_tool"))
	require.Equal(t, "res://full/note", s.TransformURI("res://full/note"))
	require.Equal(t, "plain", s.TransformURI("plain"))
}

// --- registry unit tests ---

func newMinimalSingleProxy(t *testing.T) *SingleProxy {
	t.Helper()
	cl := client.NewClient(config.UpstreamConfig{Name: "test", Transport: "streamablehttp"})
	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig), nil, "")
	authorizer, err := auth.NewAuthorizer(make(map[string][]string), []auth.AuthRule{})
	require.NoError(t, err)
	proxy, err := NewSingleProxy("streamablehttp", cl, "", authenticator, authorizer)
	require.NoError(t, err)
	return proxy
}

func TestSingleProxy_RegistryMethods(t *testing.T) {
	proxy := newMinimalSingleProxy(t)

	// Empty initially
	require.Nil(t, proxy.GetToolNames())
	require.Nil(t, proxy.GetResourceURIs())
	require.Nil(t, proxy.GetResourceTemplateURIs())
	require.Nil(t, proxy.GetPromptNames())

	proxy.AddToolName("echo")
	proxy.AddResourceURI("res://full/note")
	proxy.AddResourceTemplateURI("res://full/{id}")
	proxy.AddPromptName("greet")

	require.Equal(t, []string{"echo"}, proxy.GetToolNames())
	require.Equal(t, []string{"res://full/note"}, proxy.GetResourceURIs())
	require.Equal(t, []string{"res://full/{id}"}, proxy.GetResourceTemplateURIs())
	require.Equal(t, []string{"greet"}, proxy.GetPromptNames())

	// ClearAll resets everything (RemoveTools/Resources/etc on empty server is a no-op)
	proxy.ClearAll()
	require.Nil(t, proxy.GetToolNames())
	require.Nil(t, proxy.GetResourceURIs())
	require.Nil(t, proxy.GetResourceTemplateURIs())
	require.Nil(t, proxy.GetPromptNames())
}

func TestSingleProxy_ClearAll_NoopWhenEmpty(t *testing.T) {
	proxy := newMinimalSingleProxy(t)
	// ClearAll on an empty registry must not panic
	require.NotPanics(t, proxy.ClearAll)
}

func TestMultiProxy_RegistryMethods(t *testing.T) {
	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig), nil, "")
	authorizer, err := auth.NewAuthorizer(make(map[string][]string), []auth.AuthRule{})
	require.NoError(t, err)
	mp, err := NewMultiProxy("streamablehttp", make(map[string]*client.Client), nil, "", authenticator, authorizer)
	require.NoError(t, err)

	reg := &multiProxyClientRegistry{proxy: mp, clientName: "svc"}

	require.Nil(t, reg.GetToolNames())
	require.Nil(t, reg.GetResourceURIs())
	require.Nil(t, reg.GetResourceTemplateURIs())
	require.Nil(t, reg.GetPromptNames())

	reg.AddToolName("echo")
	reg.AddResourceURI("res://full/note")
	reg.AddResourceTemplateURI("res://full/{id}")
	reg.AddPromptName("greet")

	require.Equal(t, []string{"echo"}, reg.GetToolNames())
	require.Equal(t, []string{"res://full/note"}, reg.GetResourceURIs())
	require.Equal(t, []string{"res://full/{id}"}, reg.GetResourceTemplateURIs())
	require.Equal(t, []string{"greet"}, reg.GetPromptNames())

	reg.ClearAll()
	require.Nil(t, reg.GetToolNames())
	require.Nil(t, reg.GetResourceURIs())
	require.Nil(t, reg.GetResourceTemplateURIs())
	require.Nil(t, reg.GetPromptNames())
}

// --- resources and prompts integration tests ---

func newUpstreamClientFor(t *testing.T, ctx context.Context, handler http.Handler, name string) *client.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cl := client.NewClient(config.UpstreamConfig{
		Name:      name,
		Transport: "streamablehttp",
		URL:       srv.URL + "/mcp",
	})
	cl.Start(ctx)
	require.NoError(t, cl.WaitForConnection(ctx))
	t.Cleanup(func() { cl.Close() }) //nolint:errcheck
	return cl
}

func TestSingleProxy_ResourcesAndPrompts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	handler, err := (&mcptest.MCPFullServer{}).GetHTTPHandler()
	require.NoError(t, err)
	upstreamClient := newUpstreamClientFor(t, ctx, handler, "full")

	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig), nil, "")
	authorizer, err := auth.NewAuthorizer(make(map[string][]string), []auth.AuthRule{})
	require.NoError(t, err)

	proxy, err := NewSingleProxy("streamablehttp", upstreamClient, "", authenticator, authorizer)
	require.NoError(t, err)
	require.NoError(t, proxy.Init(ctx))

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1.0.0"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: proxyServer.URL + "/mcp"}, nil)
	require.NoError(t, err)
	defer session.Close()

	// Resources
	resResult, err := session.ListResources(ctx, &mcp.ListResourcesParams{})
	require.NoError(t, err)
	require.Len(t, resResult.Resources, 1)
	require.Equal(t, "res://full/note", resResult.Resources[0].URI)

	readResult, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "res://full/note"})
	require.NoError(t, err)
	require.Equal(t, "note content", readResult.Contents[0].Text)

	// Resource templates
	tmplResult, err := session.ListResourceTemplates(ctx, &mcp.ListResourceTemplatesParams{})
	require.NoError(t, err)
	require.Len(t, tmplResult.ResourceTemplates, 1)
	require.Equal(t, "res://full/{id}", tmplResult.ResourceTemplates[0].URITemplate)

	// Prompts
	promptsResult, err := session.ListPrompts(ctx, &mcp.ListPromptsParams{})
	require.NoError(t, err)
	require.Len(t, promptsResult.Prompts, 1)
	require.Equal(t, "greet", promptsResult.Prompts[0].Name)

	getPromptResult, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "greet"})
	require.NoError(t, err)
	require.Len(t, getPromptResult.Messages, 1)
	require.Equal(t, "Hello!", getPromptResult.Messages[0].Content.(*mcp.TextContent).Text)

	// Verify registry was populated
	require.Equal(t, []string{"res://full/note"}, proxy.GetResourceURIs())
	require.Equal(t, []string{"res://full/{id}"}, proxy.GetResourceTemplateURIs())
	require.Equal(t, []string{"greet"}, proxy.GetPromptNames())
}

func TestMultiProxy_ResourcesAndPrompts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	handler, err := (&mcptest.MCPFullServer{}).GetHTTPHandler()
	require.NoError(t, err)
	upstreamClient := newUpstreamClientFor(t, ctx, handler, "full")

	clients := map[string]*client.Client{"full": upstreamClient}
	upstreams := []config.MultiUpstreamConfig{{Name: "full", Prefix: "svc"}}

	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig), nil, "")
	authorizer, err := auth.NewAuthorizer(make(map[string][]string), []auth.AuthRule{})
	require.NoError(t, err)

	proxy, err := NewMultiProxy("streamablehttp", clients, upstreams, "", authenticator, authorizer)
	require.NoError(t, err)
	require.NoError(t, proxy.Init(ctx))

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1.0.0"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: proxyServer.URL + "/mcp"}, nil)
	require.NoError(t, err)
	defer session.Close()

	// Resources are transformed: res://full/note -> res://svc/full/note
	resResult, err := session.ListResources(ctx, &mcp.ListResourcesParams{})
	require.NoError(t, err)
	require.Len(t, resResult.Resources, 1)
	require.Equal(t, "res://svc/full/note", resResult.Resources[0].URI)

	// Resource templates: res://full/{id} -> res://svc/full/{id}
	tmplResult, err := session.ListResourceTemplates(ctx, &mcp.ListResourceTemplatesParams{})
	require.NoError(t, err)
	require.Len(t, tmplResult.ResourceTemplates, 1)
	require.Equal(t, "res://svc/full/{id}", tmplResult.ResourceTemplates[0].URITemplate)

	// Tool name is transformed: echo -> svc_echo
	toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	require.NoError(t, err)
	require.Len(t, toolsResult.Tools, 1)
	require.Equal(t, "svc_echo", toolsResult.Tools[0].Name)

	// Prompt name is transformed: greet -> svc_greet
	promptsResult, err := session.ListPrompts(ctx, &mcp.ListPromptsParams{})
	require.NoError(t, err)
	require.Len(t, promptsResult.Prompts, 1)
	require.Equal(t, "svc_greet", promptsResult.Prompts[0].Name)
}

// --- refresh / clear handler tests ---

func TestSingleProxy_RefreshHandlers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	calcServer := &mcptest.MCPCalculator{}
	handler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err)
	upstreamClient := newUpstreamClientFor(t, ctx, handler, "calculator")

	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig), nil, "")
	authorizer, err := auth.NewAuthorizer(make(map[string][]string), []auth.AuthRule{})
	require.NoError(t, err)

	proxy, err := NewSingleProxy("streamablehttp", upstreamClient, "", authenticator, authorizer)
	require.NoError(t, err)
	require.NoError(t, proxy.Init(ctx))

	// Initial registry state: "calculate" registered
	require.Equal(t, []string{"calculate"}, proxy.GetToolNames())

	// Rename upstream tool — triggers tools/list_changed → refreshHandlers
	require.NoError(t, calcServer.RenameCalculateToCount(ctx))

	// Proxy registry must eventually reflect "count" after the async refresh
	require.Eventually(t, func() bool {
		tools := proxy.GetToolNames()
		return len(tools) == 1 && tools[0] == "count"
	}, 5*time.Second, 50*time.Millisecond, "proxy should refresh tools after upstream rename")
}

func TestMultiProxy_ClearAndRefreshHandlers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	calcServer := &mcptest.MCPCalculator{}
	handler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err)
	upstreamClient := newUpstreamClientFor(t, ctx, handler, "calculator")

	clients := map[string]*client.Client{"calculator": upstreamClient}
	upstreams := []config.MultiUpstreamConfig{{Name: "calculator", Prefix: "calc"}}

	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig), nil, "")
	authorizer, err := auth.NewAuthorizer(make(map[string][]string), []auth.AuthRule{})
	require.NoError(t, err)

	proxy, err := NewMultiProxy("streamablehttp", clients, upstreams, "", authenticator, authorizer)
	require.NoError(t, err)
	require.NoError(t, proxy.Init(ctx))

	// After Init, "calc_calculate" is registered
	proxy.mu.RLock()
	tools := proxy.registeredTools["calculator"]
	proxy.mu.RUnlock()
	require.Equal(t, []string{"calc_calculate"}, tools)

	// clearClientHandlers removes the entry
	proxy.clearClientHandlers("calculator")

	proxy.mu.RLock()
	_, exists := proxy.registeredTools["calculator"]
	proxy.mu.RUnlock()
	require.False(t, exists)

	// Rename upstream tool — triggers tools/list_changed → refreshHandlers goroutine
	require.NoError(t, calcServer.RenameCalculateToCount(ctx))

	// Registry must eventually reflect "calc_count" after the async refresh
	require.Eventually(t, func() bool {
		proxy.mu.RLock()
		tools := proxy.registeredTools["calculator"]
		proxy.mu.RUnlock()
		return len(tools) == 1 && tools[0] == "calc_count"
	}, 5*time.Second, 50*time.Millisecond, "multiproxy should refresh and register calc_count")
}
