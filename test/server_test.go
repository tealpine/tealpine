package test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"tealpine/pkg/config"
	"tealpine/pkg/server"
)

// bearerAuthTransport wraps an http.RoundTripper to add Bearer token authorization
type bearerAuthTransport struct {
	wrapped http.RoundTripper
	bearer  string
}

func (t *bearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "Bearer "+t.bearer)
	return t.wrapped.RoundTrip(req2)
}

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
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: serverAddr,
		},
		MCP: map[string]config.MCPConfig{
			"calculator": {
				Name:      "calculator",
				Transport: "streamablehttp",
				URL:       calcUpstreamServer.URL + "/mcp",
				Bearer:    "upstream-calc-token",
			},
			"temperature": {
				Name:      "temperature",
				Transport: "streamablehttp",
				URL:       tempUpstreamServer.URL + "/mcp",
				Bearer:    "upstream-temp-token",
			},
			"hello": {
				Name:      "hello",
				Transport: "stdio",
				Cmd:       "go",
				CmdArgs: []string{
					"run",
					"mcp_servers/main.go",
					"server",
					"hello",
				},
			},
		},
		Proxy: map[string]config.ProxyConfig{
			"calc": {
				Name:      "calc",
				Path:      "calc",
				Transport: "streamablehttp",
				MCP:       "calculator",
				Auth: []config.AuthRule{
					{
						User:   "test-user",
						Method: "tools/list",
						Allow:  []string{"*"},
					},
					{
						User:   "test-user",
						Method: "tools/call",
						Allow:  []string{"*"},
					},
				},
			},
			"temp": {
				Name:      "temp",
				Path:      "temp",
				Transport: "streamablehttp",
				MCP:       "temperature",
				Auth: []config.AuthRule{
					{
						User:   "test-user",
						Method: "tools/list",
						Allow:  []string{"*"},
					},
					{
						User:   "test-user",
						Method: "tools/call",
						Allow:  []string{"*"},
					},
				},
			},
			"hello": {
				Name:      "hello",
				Path:      "hello",
				Transport: "streamablehttp",
				MCP:       "hello",
				Auth: []config.AuthRule{
					{
						Group:  "admins",
						Method: "tools/list",
						Allow:  []string{"*"},
					},
					{
						Group:  "admins",
						Method: "tools/call",
						Allow:  []string{"*"},
					},
				},
			},
			"multi": {
				Name:      "multi",
				Path:      "multi",
				Transport: "streamablehttp",
				MCPs: []config.MultiMCPConfig{
					{Name: "calculator", Prefix: "calc"},
					{Name: "temperature", Prefix: "temp"},
					{Name: "hello", Prefix: "hello"},
				},
				Auth: []config.AuthRule{
					{
						Group:  "power-users",
						Method: "tools/list",
						Allow:  []string{"*"},
					},
					{
						Group:  "power-users",
						Method: "tools/call",
						Allow:  []string{"*"},
					},
					{
						User:   "alice",
						Method: "tools/list",
						Allow:  []string{"*"},
					},
					{
						User:   "alice",
						Method: "tools/call",
						Allow:  []string{"calc*", "temp_get_room_temperature"},
					},
				},
			},
		},
		Users: map[string]config.UserConfig{
			"test-user": {
				Token:  "test-user-token",
				Groups: []string{"admins", "power-users"},
			},
			"alice": {
				Token:  "alice-token",
				Groups: []string{},
			},
		},
	}

	// 4. Create and initialize server
	s := server.NewServer(cfg)
	err = s.Init(ctx)
	require.NoError(t, err)

	// 5. Start server in goroutine
	serverErrChan := make(chan error, 1)
	go func() {
		if err := s.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrChan <- err
		}
	}()

	// Wait for all clients to connect
	err = s.WaitForClients(ctx)
	require.NoError(t, err)

	// 6. Ensure server cleanup
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := s.Stop(stopCtx); err != nil {
			t.Logf("Error stopping server: %v", err)
		}
	}()

	// Test "calc" proxy (streamablehttp)
	t.Run("SingleProxy_Calculator_StreamableHTTP", func(t *testing.T) {
		client := mcp.NewClient(&mcp.Implementation{
			Name:    "test-calc-client",
			Version: "1.0.0",
		}, nil)

		transport := &mcp.StreamableClientTransport{
			Endpoint: "http://" + serverAddr + "/calc/mcp",
			HTTPClient: &http.Client{
				Transport: &bearerAuthTransport{
					wrapped: http.DefaultTransport,
					bearer:  "test-user-token",
				},
			},
		}

		session, err := client.Connect(ctx, transport, nil)
		require.NoError(t, err)
		defer session.Close()

		// List all tools
		toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		require.NoError(t, err)
		require.Len(t, toolsResult.Tools, 1)
		require.Equal(t, "calculate", toolsResult.Tools[0].Name)

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "add",
				"x":         1,
				"y":         2,
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "3.00", result.Content[0].(*mcp.TextContent).Text)
	})

	// Test "temp" proxy (streamablehttp)
	t.Run("SingleProxy_Temperature_StreamableHTTP", func(t *testing.T) {
		client := mcp.NewClient(&mcp.Implementation{
			Name:    "test-temp-client",
			Version: "1.0.0",
		}, nil)

		transport := &mcp.StreamableClientTransport{
			Endpoint: "http://" + serverAddr + "/temp/mcp",
			HTTPClient: &http.Client{
				Transport: &bearerAuthTransport{
					wrapped: http.DefaultTransport,
					bearer:  "test-user-token",
				},
			},
		}

		session, err := client.Connect(ctx, transport, nil)
		require.NoError(t, err)
		defer session.Close()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "kitchen",
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Contains(t, result.Content[0].(*mcp.TextContent).Text, "The temperature in the kitchen is")
	})

	// Test "hello" proxy (streamablehttp)
	t.Run("SingleProxy_Hello_StreamableHTTP", func(t *testing.T) {
		client := mcp.NewClient(&mcp.Implementation{
			Name:    "test-hello-client",
			Version: "1.0.0",
		}, nil)

		transport := &mcp.StreamableClientTransport{
			Endpoint: "http://" + serverAddr + "/hello/mcp",
			HTTPClient: &http.Client{
				Transport: &bearerAuthTransport{
					wrapped: http.DefaultTransport,
					bearer:  "test-user-token",
				},
			},
		}

		session, err := client.Connect(ctx, transport, nil)
		require.NoError(t, err)
		defer session.Close()

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "hello_world",
			Arguments: map[string]interface{}{
				"name": "Server",
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "Hello, Server!", result.Content[0].(*mcp.TextContent).Text)
	})

	// Test "multi" proxy (streamablehttp)
	t.Run("MultiProxy_All_StreamableHTTP", func(t *testing.T) {
		client := mcp.NewClient(&mcp.Implementation{
			Name:    "test-multi-client",
			Version: "1.0.0",
		}, nil)

		transport := &mcp.StreamableClientTransport{
			Endpoint: "http://" + serverAddr + "/multi/mcp",
			HTTPClient: &http.Client{
				Transport: &bearerAuthTransport{
					wrapped: http.DefaultTransport,
					bearer:  "test-user-token",
				},
			},
		}

		session, err := client.Connect(ctx, transport, nil)
		require.NoError(t, err)
		defer session.Close()

		// List all tools
		toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
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
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "calc_calculate",
			Arguments: map[string]interface{}{
				"operation": "subtract",
				"x":         10,
				"y":         3,
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "7.00", result.Content[0].(*mcp.TextContent).Text)

		// Temp
		result, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "temp_get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "bedroom",
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Contains(t, result.Content[0].(*mcp.TextContent).Text, "The temperature in the bedroom is")

		// Hello
		result, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "hello_hello_world",
			Arguments: map[string]interface{}{
				"name": "Final Test",
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "Hello, Final Test!", result.Content[0].(*mcp.TextContent).Text)
	})

	// Test MultiProxy with upstream tools list changed notification
	t.Run("MultiProxy_UpstreamChangedTools", func(t *testing.T) {
		// Track if we received tools/list_changed notification
		toolsListChanged := make(chan struct{}, 1)

		client := mcp.NewClient(&mcp.Implementation{
			Name:    "test-multi-notification-client",
			Version: "1.0.0",
		}, &mcp.ClientOptions{
			ToolListChangedHandler: func(ctx context.Context, req *mcp.ToolListChangedRequest) {
				t.Log("Received tools/list_changed notification")
				select {
				case toolsListChanged <- struct{}{}:
				default:
				}
			},
		})

		transport := &mcp.StreamableClientTransport{
			Endpoint: "http://" + serverAddr + "/multi/mcp",
			HTTPClient: &http.Client{
				Transport: &bearerAuthTransport{
					wrapped: http.DefaultTransport,
					bearer:  "test-user-token",
				},
			},
		}

		session, err := client.Connect(ctx, transport, nil)
		require.NoError(t, err)
		defer session.Close()

		// 1. List initial tools - should see calc_calculate
		toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		require.NoError(t, err)
		require.Len(t, toolsResult.Tools, 4)
		toolNames := make(map[string]bool)
		for _, tool := range toolsResult.Tools {
			println("A", tool.Name)
			toolNames[tool.Name] = true
		}
		require.True(t, toolNames["calc_calculate"], "Should have calc_calculate tool initially")
		require.False(t, toolNames["calc_count"], "Should not have calc_count tool initially")

		// 2. Rename the tool (this will trigger tools/list_changed notification)
		err = calcServer.RenameCalculateToCount(ctx)
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)

		// 3. Wait for tools/list_changed notification
		select {
		case <-toolsListChanged:
			t.Log("Successfully received tools/list_changed notification")
		case <-time.After(3 * time.Second):
			t.Fatal("Timeout waiting for tools/list_changed notification")
		}

		// 4. List tools again - should see calc_count instead of calc_calculate
		toolsResult, err = session.ListTools(ctx, &mcp.ListToolsParams{})
		require.NoError(t, err)
		require.Len(t, toolsResult.Tools, 4)
		toolNames = make(map[string]bool)
		for _, tool := range toolsResult.Tools {
			println("B", tool.Name)
			toolNames[tool.Name] = true
		}
		require.False(t, toolNames["calc_calculate"], "Should not have calc_calculate tool after rename")
		require.True(t, toolNames["calc_count"], "Should have calc_count tool after rename")

		// 5. Call the new tool to verify it works
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "calc_count",
			Arguments: map[string]interface{}{
				"operation": "multiply",
				"x":         6,
				"y":         7,
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Equal(t, "42.00", result.Content[0].(*mcp.TextContent).Text)
	})

	// Test MultiProxy with Alice's partial permissions
	t.Run("MultiProxy_Permissions", func(t *testing.T) {
		client := mcp.NewClient(&mcp.Implementation{
			Name:    "test-alice-client",
			Version: "1.0.0",
		}, nil)

		transport := &mcp.StreamableClientTransport{
			Endpoint: "http://" + serverAddr + "/multi/mcp",
			HTTPClient: &http.Client{
				Transport: &bearerAuthTransport{
					wrapped: http.DefaultTransport,
					bearer:  "alice-token",
				},
			},
		}

		session, err := client.Connect(ctx, transport, nil)
		require.NoError(t, err)
		defer session.Close()

		// 1. List all tools - should only see tools Alice is allowed to call
		// Alice can call: "calc*" and "temp_get_room_temperature"
		toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		require.NoError(t, err)
		require.Len(t, toolsResult.Tools, 2, "Alice should only see 2 tools (filtered by permissions)")
		toolNames := make(map[string]bool)
		hasCalcTool := false
		for _, tool := range toolsResult.Tools {
			toolNames[tool.Name] = true
			// Check if any tool starts with "calc_" (calc_calculate or calc_count)
			if len(tool.Name) >= 5 && tool.Name[:5] == "calc_" {
				hasCalcTool = true
			}
		}
		require.True(t, hasCalcTool, "Alice should see a calc tool (calc_calculate or calc_count)")
		require.True(t, toolNames["temp_get_room_temperature"], "Alice should see temp_get_room_temperature")
		require.False(t, toolNames["temp_get_all_temperatures"], "Alice should NOT see temp_get_all_temperatures")
		require.False(t, toolNames["hello_hello_world"], "Alice should NOT see hello_hello_world")

		// 2. Call temp_get_room_temperature - should succeed
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "temp_get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "bedroom",
			},
		})
		require.NoError(t, err, "Alice should be able to call temp_get_room_temperature")
		require.False(t, result.IsError)
		require.Contains(t, result.Content[0].(*mcp.TextContent).Text, "The temperature in the bedroom is")

		// 3. Call temp_get_all_temperatures - should fail (not authorized)
		_, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "temp_get_all_temperatures",
			Arguments: map[string]interface{}{},
		})
		require.Error(t, err, "Alice should NOT be able to call temp_get_all_temperatures")
		require.Contains(t, err.Error(), "access denied", "Error should indicate access denied")

		// 4. Call hello_hello_world - should fail (not authorized)
		_, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "hello_hello_world",
			Arguments: map[string]interface{}{
				"name": "Alice",
			},
		})
		require.Error(t, err, "Alice should NOT be able to call hello_hello_world")
		require.Contains(t, err.Error(), "access denied", "Error should indicate access denied")
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
	upstreamURL := "http://" + upstreamAddr + "/mcp"

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
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: serverAddr,
		},
		MCP: map[string]config.MCPConfig{
			"calculator": {
				Name:           "calculator",
				Transport:      "streamablehttp",
				URL:            upstreamURL,
				Bearer:         "upstream-calc-token",
				ReconnectDelay: 500 * time.Millisecond,
				PingInterval:   500 * time.Millisecond,
			},
		},
		Proxy: map[string]config.ProxyConfig{
			"calc": {
				Name:      "calc",
				Path:      "calc",
				Transport: "streamablehttp",
				MCP:       "calculator",
				Auth: []config.AuthRule{
					{
						User:   "test-user",
						Method: "tools/call",
						Allow:  []string{"calculate"},
					},
				},
			},
		},
		Users: map[string]config.UserConfig{
			"test-user": {
				Token:  "test-user-token",
				Groups: []string{},
			},
		},
	}

	// 4. Create and initialize proxy server
	s := server.NewServer(cfg)
	err = s.Init(ctx)
	require.NoError(t, err)

	// 5. Start proxy server in goroutine
	logrus.Info("Starting proxy server")
	go func() {
		if err := s.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.Errorf("proxy server failed to start: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Ensure proxy server cleanup
	defer func() {
		if err := s.Stop(ctx); err != nil {
			logrus.Errorf("Error stopping server: %v", err)
		}
	}()

	// 6. Create client and test initial connection
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-restart-client",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint: "http://" + serverAddr + "/calc/mcp",
		HTTPClient: &http.Client{
			Transport: &bearerAuthTransport{
				wrapped: http.DefaultTransport,
				bearer:  "test-user-token",
			},
		},
	}

	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err)
	defer func() {
		if err := session.Close(); err != nil {
			logrus.Errorf("Error closing session: %v", err)
		}
	}()

	// 7. Query tool - should succeed
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
	_, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "calculate",
		Arguments: map[string]interface{}{
			"operation": "multiply",
			"x":         4,
			"y":         2,
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
	callParams := &mcp.CallToolParams{
		Name: "calculate",
		Arguments: map[string]interface{}{
			"operation": "subtract",
			"x":         10,
			"y":         4,
		},
	}

	result, err = session.CallTool(ctx, callParams)
	require.NoError(t, err)

	require.False(t, result.IsError)
	require.Equal(t, "6.00", result.Content[0].(*mcp.TextContent).Text)
	logrus.Info("Test done")
}
