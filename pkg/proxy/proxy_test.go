package proxy

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	mgclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
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
		Transport: "sse",
		URL:       upstreamServer.URL + "/sse",
	}

	// Initialize upstream client
	upstreamClient := NewClient(upstreamConfig)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	upstreamClient.Start(ctx)

	err = upstreamClient.WaitForConnection(ctx)
	require.NoError(t, err)
	defer upstreamClient.Close()

	// Create auth middleware (no auth rules for test)
	authMiddleware, err := auth.NewAuth(make(map[string]*auth.UserInfo), []auth.AuthRule{})
	require.NoError(t, err)

	// Create proxy
	proxy := NewSingleProxy("streamablehttp", upstreamClient, "", authMiddleware)
	err = proxy.Init(ctx)
	require.NoError(t, err)

	// Create httptest server for proxy
	httpServer := httptest.NewServer(proxy)
	defer httpServer.Close()

	// Create proxy client
	proxyClient, err := mgclient.NewStreamableHttpClient(httpServer.URL + "/mcp")
	require.NoError(t, err)

	err = proxyClient.Start(ctx)
	require.NoError(t, err)
	defer proxyClient.Close()

	_, err = proxyClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		},
	})
	require.NoError(t, err)

	// Test tool call
	result, err := proxyClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "add",
				"x":         20,
				"y":         22,
			},
		},
	})

	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "42.00", result.Content[0].(mcp.TextContent).Text)
}

func TestProxySseTemperature(t *testing.T) {
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

	// Create auth middleware (no auth rules for test)
	authMiddleware, err := auth.NewAuth(make(map[string]*auth.UserInfo), []auth.AuthRule{})
	require.NoError(t, err)

	// Create proxy
	proxy := NewSingleProxy("sse", upstreamClient, "", authMiddleware)
	err = proxy.Init(ctx)
	require.NoError(t, err)

	// Create httptest server for proxy
	httpServer := httptest.NewServer(proxy)
	defer httpServer.Close()

	// Create proxy client
	proxyClient, err := mgclient.NewSSEMCPClient(httpServer.URL + "/sse")
	require.NoError(t, err)

	err = proxyClient.Start(ctx)
	require.NoError(t, err)
	defer proxyClient.Close()

	_, err = proxyClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		},
	})
	require.NoError(t, err)

	// Test tool call
	result, err := proxyClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "bedroom",
			},
		},
	})

	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Contains(t, result.Content[0].(mcp.TextContent).Text, "The temperature in the bedroom is")
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
		Transport: "sse",
		URL:       calcUpstreamServer.URL + "/sse",
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

	// 4. Create auth middleware (no auth rules for test)
	authMiddleware, err := auth.NewAuth(make(map[string]*auth.UserInfo), []auth.AuthRule{})
	require.NoError(t, err)

	// 5. Create MultiProxy
	proxy := NewMultiProxy("streamablehttp", clients, multiProxyConfig, "", authMiddleware)
	err = proxy.Init(ctx)
	require.NoError(t, err)

	// 5. Start test server
	httpServer := httptest.NewServer(proxy)
	defer httpServer.Close()

	// 6. Create client for the proxy
	proxyClient, err := mgclient.NewStreamableHttpClient(httpServer.URL + "/mcp")
	require.NoError(t, err)
	err = proxyClient.Start(ctx)
	require.NoError(t, err)
	defer proxyClient.Close()

	_, err = proxyClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		},
	})
	require.NoError(t, err)

	// 7. Call tools
	// Calculator
	result, err := proxyClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calc_calculate", // Prefixed name
			Arguments: map[string]interface{}{
				"operation": "multiply",
				"x":         6,
				"y":         7,
			},
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "42.00", result.Content[0].(mcp.TextContent).Text)

	// Temperature
	result, err = proxyClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "temp_get_room_temperature", // Prefixed name
			Arguments: map[string]interface{}{
				"room": "living room",
			},
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Contains(t, result.Content[0].(mcp.TextContent).Text, "The temperature in the living room is")

	// Hello
	result, err = proxyClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "hello_hello_world", // Prefixed name
			Arguments: map[string]interface{}{
				"name": "Multi-Proxy",
			},
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "Hello, Multi-Proxy!", result.Content[0].(mcp.TextContent).Text)
}
