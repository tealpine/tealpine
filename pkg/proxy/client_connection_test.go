package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	mgclient "github.com/mark3labs/mcp-go/client"
	mcptest "mcp-auth-proxy/test"
)

// MockMCPClient is a mock implementation of mgclient.MCPClient for testing purposes.
type MockMCPClient struct {
	mgclient.MCPClient
	CallToolFunc  func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	ListToolsFunc func(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CloseFunc     func() error
}

func (m *MockMCPClient) CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if m.CallToolFunc != nil {
		return m.CallToolFunc(ctx, request)
	}
	return nil, errors.New("CallTool not implemented in mock")
}

func (m *MockMCPClient) ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	if m.ListToolsFunc != nil {
		return m.ListToolsFunc(ctx, request)
	}
	// Default successful response for ListTools to avoid panics during health checks
	return &mcp.ListToolsResult{}, nil
}

func (m *MockMCPClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

//func TestClient_CallTool_TransportErrorDisconnectsClient(t *testing.T) {
//	client := NewClient(config)
//	// Stop the health check goroutine immediately to prevent interference
//	client.Close() // Close also closes doneCh, stopping the health check
//	// Re-initialize client.isClosed to false for the test's purpose
//	client.mutex.Lock()
//	client.isClosed = false
//	client.mutex.Unlock()
//
//	// 2. Create a mock MCPClient that returns an error on CallTool
//	mockErr := errors.New("simulated transport error")
//	mockMCPClient := &MockMCPClient{
//		CallToolFunc: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
//			return nil, mockErr
//		},
//		CloseFunc: func() error {
//			return nil
//		},
//	}
//
//	// 3. Inject the mock client and set initial connected state
//	client.mutex.Lock()
//	client.client = mockMCPClient
//	client.isConnected = true // Manually set to connected for the test
//	client.mutex.Unlock()
//
//	require.True(t, client.isConnected, "Client should be initially connected")
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	// 4. Call CallTool, which should trigger the mock error and retries
//	_, err := client.CallTool(ctx, mcp.CallToolRequest{
//		Params: mcp.CallToolParams{
//			Name: "test_tool",
//		},
//	})
//
//	// 5. Assertions
//	require.Error(t, err, "CallTool should return an error due to transport error")
//	require.Contains(t, err.Error(), "request failed after multiple retries", "Error message should indicate retries failed")
//
//	// Give a small moment for the setConnected(false) to propagate if it's on a different goroutine
//	// In this case, execute is synchronous, so it should be immediate.
//	time.Sleep(10 * time.Millisecond)
//
//	client.mutex.Lock()
//	isConnectedAfterError := client.isConnected
//	client.mutex.Unlock()
//
//	require.False(t, isConnectedAfterError, "Client should be disconnected after CallTool transport error and retries")
//}

func TestClient_Connection_Management(t *testing.T) {
	// 1. Setup a controllable server
	serverOnline := true
	var serverMutex sync.Mutex
	tempServer := &mcptest.MCPTemperature{}
	handler, err := tempServer.GetHTTPHandler()
	require.NoError(t, err)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverMutex.Lock()
		online := serverOnline
		serverMutex.Unlock()

		if online {
			handler.ServeHTTP(w, r)
		} else {
			// Simulate server being down by closing the connection
			// This is more realistic than just returning an error code
			conn, _, _ := w.(http.Hijacker).Hijack()
			err := conn.Close()
			require.NoError(t, err)
		}
	}))
	defer testServer.Close()

	// 2. Configure the client
	config := MCPConfig{
		Name:           "temperature-test",
		Transport:      "streamablehttp",
		URL:            testServer.URL + "/mcp",
		PingInterval:   1 * time.Second, // Frequent pings for testing
		ReconnectDelay: 1 * time.Second, // Quick reconnect attempts
	}

	client := NewClient(config)
	defer func() {
		err := client.Close()
		require.NoError(t, err)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 3. Initial connection - should succeed
	_, err = client.Init(ctx)
	require.NoError(t, err, "Initial client initialization should succeed")
	require.True(t, client.isConnected, "Client should be marked as connected after successful init")

	// 4. First tool call - should succeed
	_, err = client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "bedroom",
			},
		},
	})
	require.NoError(t, err, "First tool call should succeed when connected")

	// 5. Take the server offline
	serverMutex.Lock()
	serverOnline = false
	serverMutex.Unlock()
	t.Log("Server is now OFFLINE")

	// 6. Wait for the client to detect disconnection via ping
	time.Sleep(2 * config.PingInterval) // Wait for at least one ping to fail
	require.False(t, client.isConnected, "Client should be marked as disconnected after ping fails")

	// 7. Tool call while disconnected - should fail immediately
	_, err = client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "bedroom",
			},
		},
	})
	require.Error(t, err, "Tool call should fail when client is disconnected")
	require.Equal(t, ErrDisconnected, err, "Error should be ErrDisconnected")

	// 8. Bring the server back online
	serverMutex.Lock()
	serverOnline = true
	serverMutex.Unlock()
	t.Log("Server is now ONLINE")

	// 9. Wait for the client to reconnect
	time.Sleep(2 * config.ReconnectDelay) // Wait for a reconnect attempt
	require.True(t, client.isConnected, "Client should be reconnected after server comes back online")

	// 10. Final tool call - should succeed again
	_, err = client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "living room",
			},
		},
	})
	require.NoError(t, err, "Tool call should succeed after reconnection")
}
