package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"mcp-auth-proxy/pkg/auth"
	"mcp-auth-proxy/pkg/client"
	"mcp-auth-proxy/pkg/config"
	mcptest "mcp-auth-proxy/test"
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
	upstreamConfig := config.MCPConfig{
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
	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig))
	authorizer, err := auth.NewAuthorizer(make(map[string]config.UserConfig), []auth.AuthRule{})
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
	upstreamConfig := config.MCPConfig{
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
	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig))
	authorizer, err := auth.NewAuthorizer(make(map[string]config.UserConfig), []auth.AuthRule{})
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
	calcConfig := config.MCPConfig{
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
	tempConfig := config.MCPConfig{
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
	helloConfig := config.MCPConfig{
		Name:      "hello",
		Transport: "stdio",
		Cmd:       "go",
		CmdArgs: []string{
			"run",
			"../../test/mcp_servers/main.go",
			"hello",
			"server",
		},
	}
	helloClient := client.NewClient(helloConfig)
	helloClient.Start(ctx)
	err = helloClient.WaitForConnection(ctx)
	require.NoError(t, err)
	clients["hello"] = helloClient
	defer helloClient.Close()

	// 3. Create multi-proxy config
	multiProxyConfig := []config.MultiMCPConfig{
		{Name: "calculator", Prefix: "calc"},
		{Name: "temperature", Prefix: "temp"},
		{Name: "hello", Prefix: "hello"},
	}

	// 4. Create authenticator and authorizer (no auth for test)
	authenticator := auth.NewAuthenticator(make(map[string]config.UserConfig))
	authorizer, err := auth.NewAuthorizer(make(map[string]config.UserConfig), []auth.AuthRule{})
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
	upstreamConfig := config.MCPConfig{
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
			Token:  "alice-token",
			Groups: []string{},
		},
		"bob": {
			Token:  "bob-token",
			Groups: []string{},
		},
	}

	authRules := []auth.AuthRule{
		{
			User:   "alice",
			Method: "tools/list",
			Allow:  []string{"*"}, // Allow listing all tools
		},
		{
			User:   "alice",
			Method: "tools/call",
			Allow:  []string{"calculate"}, // But only allow calling "calculate"
		},
		{
			User:   "bob",
			Method: "tools/list",
			Allow:  []string{"*"}, // Allow listing
		},
		{
			User:   "bob",
			Method: "tools/call",
			Allow:  []string{"nonexistent_*"}, // But no actual tools
		},
	}

	authenticator := auth.NewAuthenticator(users)
	authorizer, err := auth.NewAuthorizer(users, authRules)
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
