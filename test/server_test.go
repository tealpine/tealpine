package test

import (
	"context"
	"errors"
	"github.com/sirupsen/logrus"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mgclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"mcp-auth-proxy/pkg/proxy"
)

func TestServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. Create upstream MCP servers with httptest
	// Calculator server (SSE)
	calcServer := &MCPCalculator{}
	calcHandler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err)
	calcUpstreamServer := httptest.NewServer(calcHandler)
	defer calcUpstreamServer.Close()

	// Temperature server (StreamableHTTP)
	tempServer := &MCPTemperature{}
	tempHandler, err := tempServer.GetHTTPHandler()
	require.NoError(t, err)
	tempUpstreamServer := httptest.NewServer(tempHandler)
	defer tempUpstreamServer.Close()

	// 2. Create a listener to get a dynamic port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	serverAddr := listener.Addr().String()
	err = listener.Close() // Close it so the server can bind to it
	require.NoError(t, err)

	// 3. Create configuration
	config := &proxy.Config{
		Server: proxy.ServerConfig{
			Host: serverAddr,
		},
		MCP: map[string]proxy.MCPConfig{
			"calculator": {
				Name:      "calculator",
				Transport: "sse",
				URL:       calcUpstreamServer.URL + "/sse",
			},
			"temperature": {
				Name:      "temperature",
				Transport: "streamablehttp",
				URL:       tempUpstreamServer.URL + "/mcp",
			},
			"hello": {
				Name:      "hello",
				Transport: "stdio",
				Cmd:       "go",
				CmdArgs: []string{
					"run",
					"mcp_servers/main.go",
					"hello",
					"server",
				},
			},
		},
		Proxy: map[string]proxy.ProxyConfig{
			"calc": {
				Name:      "calc",
				Path:      "calc",
				Transport: "streamablehttp",
				MCP:       "calculator",
			},
			"temp": {
				Name:      "temp",
				Path:      "temp",
				Transport: "sse",
				MCP:       "temperature",
			},
			"hello": {
				Name:      "hello",
				Path:      "hello",
				Transport: "sse",
				MCP:       "hello",
			},
			"multi": {
				Name:      "multi",
				Path:      "multi",
				Transport: "streamablehttp",
				MCPs: []proxy.MultiMCPConfig{
					{Name: "calculator", Prefix: "calc"},
					{Name: "temperature", Prefix: "temp"},
					{Name: "hello", Prefix: "hello"},
				},
			},
			"multisse": {
				Name:      "multisse",
				Path:      "multisse",
				Transport: "sse",
				MCPs: []proxy.MultiMCPConfig{
					{Name: "calculator", Prefix: "calc"},
					{Name: "temperature", Prefix: "temp"},
					{Name: "hello", Prefix: "hello"},
				},
			},
		},
	}

	// 4. Create and initialize server
	server := proxy.NewServer(config)
	err = server.Init(ctx)
	require.NoError(t, err)

	// 5. Start server in goroutine
	serverErrChan := make(chan error, 1)
	go func() {
		if err := server.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrChan <- err
		}
	}()

	// Give server time to start #TODO add some method which ends when all clients are connected
	time.Sleep(900 * time.Millisecond)

	// 6. Ensure server cleanup
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := server.Stop(stopCtx); err != nil {
			t.Logf("Error stopping server: %v", err)
		}
	}()

	// Test "calc" proxy (streamablehttp)
	t.Run("SingleProxy_Calculator_StreamableHTTP", func(t *testing.T) {
		calcClient, err := mgclient.NewStreamableHttpClient("http://" + serverAddr + "/calc/mcp")
		require.NoError(t, err)
		err = calcClient.Start(ctx)
		require.NoError(t, err)
		defer calcClient.Close()

		_, err = calcClient.Initialize(ctx, mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			},
		})
		require.NoError(t, err)

		// List all tools
		toolsResult, err := calcClient.ListTools(ctx, mcp.ListToolsRequest{})
		require.NoError(t, err)
		require.Len(t, toolsResult.Tools, 1)
		require.Equal(t, "calculate", toolsResult.Tools[0].Name)

		result, err := calcClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "calculate",
				Arguments: map[string]interface{}{
					"operation": "add",
					"x":         1,
					"y":         2,
				},
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "3.00", result.Content[0].(mcp.TextContent).Text)
	})

	// Test "temp" proxy (sse)
	t.Run("SingleProxy_Temperature_SSE", func(t *testing.T) {
		tempClient, err := mgclient.NewSSEMCPClient("http://" + serverAddr + "/temp/sse")
		require.NoError(t, err)
		err = tempClient.Start(ctx)
		require.NoError(t, err)
		defer tempClient.Close()

		_, err = tempClient.Initialize(ctx, mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			},
		})
		require.NoError(t, err)

		result, err := tempClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "get_room_temperature",
				Arguments: map[string]interface{}{
					"room": "kitchen",
				},
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Contains(t, result.Content[0].(mcp.TextContent).Text, "The temperature in the kitchen is")
	})

	// Test "hello" proxy (sse)
	t.Run("SingleProxy_Hello_SSE", func(t *testing.T) {
		helloClient, err := mgclient.NewSSEMCPClient("http://" + serverAddr + "/hello/sse")
		require.NoError(t, err)
		err = helloClient.Start(ctx)
		require.NoError(t, err)
		defer func() {
			err := helloClient.Close()
			require.NoError(t, err)
		}()

		_, err = helloClient.Initialize(ctx, mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			},
		})

		require.NoError(t, err)

		result, err := helloClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "hello_world",
				Arguments: map[string]interface{}{
					"name": "Server",
				},
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "Hello, Server!", result.Content[0].(mcp.TextContent).Text)
	})

	// Test "multi" proxy (streamablehttp)
	t.Run("MultiProxy_All_StreamableHTTP", func(t *testing.T) {
		multiClient, err := mgclient.NewStreamableHttpClient("http://" + serverAddr + "/multi/mcp")
		require.NoError(t, err)
		err = multiClient.Start(ctx)
		require.NoError(t, err)
		defer func() {
			err := multiClient.Close()
			require.NoError(t, err)
		}()

		_, err = multiClient.Initialize(ctx, mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			},
		})
		require.NoError(t, err)

		// List all tools
		toolsResult, err := multiClient.ListTools(ctx, mcp.ListToolsRequest{})
		require.NoError(t, err)
		require.Len(t, toolsResult.Tools, 4)
		toolNames := make(map[string]bool)
		for _, tool := range toolsResult.Tools {
			toolNames[tool.Name] = true
		}
		require.True(t, toolNames["calc_calculate"])
		require.True(t, toolNames["temp_get_all_temperatures"])
		require.True(t, toolNames["temp_get_room_temperature"])
		require.True(t, toolNames["hello_hello_world"])

		// Calc
		result, err := multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "calc_calculate",
				Arguments: map[string]interface{}{
					"operation": "subtract",
					"x":         10,
					"y":         3,
				},
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "7.00", result.Content[0].(mcp.TextContent).Text)

		// Temp
		result, err = multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "temp_get_room_temperature",
				Arguments: map[string]interface{}{
					"room": "bedroom",
				},
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Contains(t, result.Content[0].(mcp.TextContent).Text, "The temperature in the bedroom is")

		// Hello
		result, err = multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "hello_hello_world",
				Arguments: map[string]interface{}{
					"name": "Final Test",
				},
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "Hello, Final Test!", result.Content[0].(mcp.TextContent).Text)
	})

	// Test "multisse" proxy (sse)
	t.Run("MultiProxy_All_SSE", func(t *testing.T) {
		multiClient, err := mgclient.NewSSEMCPClient("http://" + serverAddr + "/multisse/sse")
		require.NoError(t, err)
		err = multiClient.Start(ctx)
		require.NoError(t, err)
		defer func() {
			err := multiClient.Close()
			require.NoError(t, err)
		}()

		_, err = multiClient.Initialize(ctx, mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			},
		})
		require.NoError(t, err)

		// Calc
		result, err := multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "calc_calculate",
				Arguments: map[string]interface{}{
					"operation": "subtract",
					"x":         10,
					"y":         3,
				},
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "7.00", result.Content[0].(mcp.TextContent).Text)

		// Temp
		result, err = multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "temp_get_room_temperature",
				Arguments: map[string]interface{}{
					"room": "bedroom",
				},
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Contains(t, result.Content[0].(mcp.TextContent).Text, "The temperature in the bedroom is")

		// Hello
		result, err = multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "hello_hello_world",
				Arguments: map[string]interface{}{
					"name": "Final Test",
				},
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "Hello, Final Test!", result.Content[0].(mcp.TextContent).Text)
	})
}

func TestServerWithUpstreamServerRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()

	// 1. Create upstream MCP server with net/http.Server on a fixed port
	calcMCPServer := &MCPCalculator{}
	calcHandler, err := calcMCPServer.GetHTTPHandler()
	require.NoError(t, err)

	// Create a listener to get a dynamic port for upstream server
	upstreamListener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	upstreamAddr := upstreamListener.Addr().String()
	upstreamURL := "http://" + upstreamAddr + "/sse"

	// Create http.Server for upstream
	calcUpstreamServer := &http.Server{
		Addr:    upstreamAddr,
		Handler: calcHandler,
	}

	// Start upstream server in goroutine
	go func() {
		if err := calcUpstreamServer.Serve(upstreamListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.Errorf("upstream server error: %v", err)
		}
	}()

	// Give upstream server time to start
	time.Sleep(100 * time.Millisecond)

	// 2. Create a listener to get a dynamic port for proxy server
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	serverAddr := listener.Addr().String()
	err = listener.Close()
	require.NoError(t, err)

	// 3. Create configuration
	config := &proxy.Config{
		Server: proxy.ServerConfig{
			Host: serverAddr,
		},
		MCP: map[string]proxy.MCPConfig{
			"calculator": {
				Name:           "calculator",
				Transport:      "sse",
				URL:            upstreamURL,
				ReconnectDelay: 500 * time.Millisecond,
				PingInterval:   500 * time.Millisecond,
			},
		},
		Proxy: map[string]proxy.ProxyConfig{
			"calc": {
				Name:      "calc",
				Path:      "calc",
				Transport: "streamablehttp",
				MCP:       "calculator",
			},
		},
	}

	// 4. Create and initialize proxy server
	server := proxy.NewServer(config)
	err = server.Init(ctx)
	require.NoError(t, err)

	// 5. Start proxy server in goroutine
	logrus.Info("Starting proxy server")
	go func() {
		if err := server.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.Errorf("proxy server failed to start: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Ensure proxy server cleanup
	defer func() {
		if err := server.Stop(ctx); err != nil {
			logrus.Errorf("Error stopping server: %v", err)
		}
	}()

	// 6. Create client and test initial connection
	client, err := mgclient.NewStreamableHttpClient("http://" + serverAddr + "/calc/mcp")
	require.NoError(t, err)
	err = client.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if err := client.Close(); err != nil {
			logrus.Errorf("Error closing client: %v", err)
		}
	}()

	_, err = client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		},
	})
	require.NoError(t, err)

	// 7. Query tool - should succeed
	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "add",
				"x":         5,
				"y":         3,
			},
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "8.00", result.Content[0].(mcp.TextContent).Text)

	// 8. Stop the upstream MCP server
	logrus.Info("Stop the upstream MCP server")
	// Close the listener first to stop accepting new connections
	err = upstreamListener.Close()
	require.NoError(t, err)
	// Then close the server forcefully
	err = calcUpstreamServer.Close()
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond) // Give time for connection to close

	// 9. Query tool again - should fail with connection error
	logrus.Info("Query closed upstream MCP server")
	_, err = client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "multiply",
				"x":         4,
				"y":         2,
			},
		},
	})
	require.Error(t, err, "Tool call should fail when upstream server is stopped")
	t.Logf("Expected error after upstream server stopped: %v", err)
	logrus.Infof("Query error %s", err.Error())

	// 10. Restart the upstream MCP server at the same address
	logrus.Info("Restart the upstream MCP server")

	// Create a new listener on the same address
	upstreamListener2, err := net.Listen("tcp", upstreamAddr)
	require.NoError(t, err)

	// Create new handler and server
	calcMCPServer2 := &MCPCalculator{}
	calcHandler2, err := calcMCPServer2.GetHTTPHandler()
	require.NoError(t, err)

	calcUpstreamServer2 := &http.Server{
		Addr:    upstreamAddr,
		Handler: calcHandler2,
	}

	// Start the restarted upstream server
	go func() {
		if err := calcUpstreamServer2.Serve(upstreamListener2); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.Errorf("restarted upstream server error: %v", err)
		}
	}()

	// Ensure cleanup of restarted server
	defer func() {
		shutdownCtx2, shutdownCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel2()
		if err := calcUpstreamServer2.Shutdown(shutdownCtx2); err != nil {
			logrus.Errorf("Error stopping restarted upstream server: %v", err)
		}
	}()

	// Give server time to start and reconnect
	logrus.Info("wait 2s")
	time.Sleep(2 * time.Second)

	// 11. Query tool again - should succeed after restart
	logrus.Info("Query tool after restart")
	callReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "subtract",
				"x":         10,
				"y":         4,
			},
		},
	}

	result, err = client.CallTool(ctx, callReq)
	require.Error(t, err) // Bug in mcp-go

	result, err = client.CallTool(ctx, callReq)
	require.NoError(t, err) // Bug in mcp-go: https://github.com/mark3labs/mcp-go/issues/638

	require.False(t, result.IsError)
	require.Equal(t, "6.00", result.Content[0].(mcp.TextContent).Text)
	logrus.Info("Test done")
}
