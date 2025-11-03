package test

import (
	"net/http/httptest"
	"testing"

	mgclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"mcp-auth-proxy/pkg/mcp_proxy"
)

func TestProxyStreamableHttpCalculator(t *testing.T) {
	config := getConfig(t)

	upstreamClient := mcp_proxy.NewClient(config.MCP["calculator"])
	_, err := upstreamClient.Init(t.Context())
	require.NoError(t, err)

	proxy := mcp_proxy.NewSingleProxy("streamablehttp", upstreamClient, "")
	err = proxy.Init(t.Context())
	require.NoError(t, err)
	defer upstreamClient.Close()

	httpServer := httptest.NewServer(proxy)
	defer httpServer.Close()

	proxyClient, err := mgclient.NewStreamableHttpClient(httpServer.URL + "/mcp")
	require.NoError(t, err)

	err = proxyClient.Start(t.Context())
	require.NoError(t, err)
	defer proxyClient.Close()

	_, err = proxyClient.Initialize(t.Context(), mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		},
	})
	require.NoError(t, err)

	result, err := proxyClient.CallTool(t.Context(), mcp.CallToolRequest{
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
	config := getConfig(t)

	upstreamClient := mcp_proxy.NewClient(config.MCP["temperature"])
	_, err := upstreamClient.Init(t.Context())
	require.NoError(t, err)

	proxy := mcp_proxy.NewSingleProxy("sse", upstreamClient, "")
	err = proxy.Init(t.Context())
	require.NoError(t, err)
	defer upstreamClient.Close()

	httpServer := httptest.NewServer(proxy)
	defer httpServer.Close()

	proxyClient, err := mgclient.NewSSEMCPClient(httpServer.URL + "/sse")
	require.NoError(t, err)

	err = proxyClient.Start(t.Context())
	require.NoError(t, err)
	defer proxyClient.Close()

	_, err = proxyClient.Initialize(t.Context(), mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		},
	})
	require.NoError(t, err)

	result, err := proxyClient.CallTool(t.Context(), mcp.CallToolRequest{
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
	config := getConfig(t)

	// 1. Create clients
	clients := make(map[string]*mcp_proxy.Client)
	for name, mcpConfig := range config.MCP {
		client := mcp_proxy.NewClient(mcpConfig)
		clients[name] = client
		_, err := client.Init(t.Context())
		require.NoError(t, err)
	}

	// 2. Get multi-proxy config
	multiProxyConfig := config.Proxy["multi"]

	// 3. Create MultiProxy
	proxy := mcp_proxy.NewMultiProxy(multiProxyConfig.Transport, clients, multiProxyConfig.MCPs, "")
	err := proxy.Init(t.Context())
	require.NoError(t, err)
	defer func() {
		for _, client := range clients {
			client.Close()
		}
	}()

	// 4. Start test server
	httpServer := httptest.NewServer(proxy)
	defer httpServer.Close()

	// 5. Create client for the proxy
	proxyClient, err := mgclient.NewStreamableHttpClient(httpServer.URL + "/mcp")
	require.NoError(t, err)
	err = proxyClient.Start(t.Context())
	require.NoError(t, err)
	defer proxyClient.Close()

	_, err = proxyClient.Initialize(t.Context(), mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		},
	})
	require.NoError(t, err)

	// 6. Call tools
	// Calculator
	result, err := proxyClient.CallTool(t.Context(), mcp.CallToolRequest{
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
	result, err = proxyClient.CallTool(t.Context(), mcp.CallToolRequest{
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
	result, err = proxyClient.CallTool(t.Context(), mcp.CallToolRequest{
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
