package proxy

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"mcp-auth-proxy/pkg/auth"
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
	upstreamConfig := MCPConfig{
		Name:      "calculator",
		Transport: "streamablehttp",
		URL:       upstreamServer.URL + "/mcp",
	}

	// Initialize upstream client
	upstreamClient := NewClient(upstreamConfig)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	upstreamClient.Start(ctx)

	err = upstreamClient.WaitForConnection(ctx)
	require.NoError(t, err)
	defer upstreamClient.Close()

	// Create authenticator and authorizer (no auth for test)
	authenticator := auth.NewAuthenticator(make(map[string]*auth.UserInfo))
	authorizer, err := auth.NewAuthorizer(make(map[string]*auth.UserInfo), []auth.AuthRule{})
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
	upstreamConfig := MCPConfig{
		Name:      "temperature",
		Transport: "streamablehttp",
		URL:       upstreamServer.URL + "/mcp",
	}

	// Initialize upstream client
	upstreamClient := NewClient(upstreamConfig)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	upstreamClient.Start(ctx)

	err = upstreamClient.WaitForConnection(ctx)
	require.NoError(t, err)
	defer upstreamClient.Close()

	// Create authenticator and authorizer (no auth for test)
	authenticator := auth.NewAuthenticator(make(map[string]*auth.UserInfo))
	authorizer, err := auth.NewAuthorizer(make(map[string]*auth.UserInfo), []auth.AuthRule{})
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
	clients := make(map[string]*Client)

	// Calculator client
	calcConfig := MCPConfig{
		Name:      "calculator",
		Transport: "streamablehttp",
		URL:       calcUpstreamServer.URL + "/mcp",
	}
	calcClient := NewClient(calcConfig)
	calcClient.Start(ctx)

	err = calcClient.WaitForConnection(ctx)
	require.NoError(t, err)
	clients["calculator"] = calcClient
	defer calcClient.Close()

	// Temperature client
	tempConfig := MCPConfig{
		Name:      "temperature",
		Transport: "streamablehttp",
		URL:       tempUpstreamServer.URL + "/mcp",
	}
	tempClient := NewClient(tempConfig)
	tempClient.Start(ctx)
	err = tempClient.WaitForConnection(ctx)
	require.NoError(t, err)
	clients["temperature"] = tempClient
	defer tempClient.Close()

	// Hello client (stdio)
	helloConfig := MCPConfig{
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
	helloClient := NewClient(helloConfig)
	helloClient.Start(ctx)
	err = helloClient.WaitForConnection(ctx)
	require.NoError(t, err)
	clients["hello"] = helloClient
	defer helloClient.Close()

	// 3. Create multi-proxy config
	multiProxyConfig := []MultiMCPConfig{
		{Name: "calculator", Prefix: "calc"},
		{Name: "temperature", Prefix: "temp"},
		{Name: "hello", Prefix: "hello"},
	}

	// 4. Create authenticator and authorizer (no auth for test)
	authenticator := auth.NewAuthenticator(make(map[string]*auth.UserInfo))
	authorizer, err := auth.NewAuthorizer(make(map[string]*auth.UserInfo), []auth.AuthRule{})
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
