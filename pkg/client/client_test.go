package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"tealpine/pkg/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	mcptest "tealpine/test"
)

func TestClientWithStreamableHTTPServer(t *testing.T) {
	// Create the MCP temperature server
	tempServer := &mcptest.MCPTemperature{}
	handler, err := tempServer.GetHTTPHandler()
	require.NoError(t, err, "Failed to get HTTP handler")

	// Create httptest server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Create client configuration pointing to the test server
	cfg := config.UpstreamConfig{
		Name:      "temperature-test",
		Transport: "streamablehttp",
		URL:       testServer.URL + "/mcp",
	}

	// Create and initialize the client
	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client.Start(ctx)

	err = client.WaitForConnection(ctx)
	require.NoError(t, err, "Failed to initialize client")
	require.NotNil(t, client.initResult, "Init result should not be nil")
	require.Equal(t, "apartment-temperature-server", client.initResult.ServerInfo.Name)

	// Test calling the get_room_temperature tool
	result, err := client.CallTool(ctx, &mcp.CallToolParams{

		Name: "get_room_temperature",
		Arguments: map[string]interface{}{
			"room": "bedroom",
		},
	},
	)
	require.NoError(t, err, "Failed to call get_room_temperature tool")
	require.NotNil(t, result, "Result should not be nil")
	require.False(t, result.IsError, "Result should not be an error")
	require.Len(t, result.Content, 1, "Result should have one content item")

	// Check that the response contains the expected text
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Contains(t, textContent.Text, "The temperature in the bedroom is", "Response should contain temperature message")
	require.Contains(t, textContent.Text, "°C", "Response should contain temperature unit")

	// Test calling with an invalid room
	invalidResult, err := client.CallTool(ctx, &mcp.CallToolParams{

		Name: "get_room_temperature",
		Arguments: map[string]interface{}{
			"room": "garage",
		},
	},
	)
	require.NoError(t, err, "Call should not return error even for invalid room")
	require.NotNil(t, invalidResult, "Result should not be nil")
	require.True(t, invalidResult.IsError, "Result should be an error for invalid room")

	// Test calling get_all_temperatures tool
	allTempsResult, err := client.CallTool(ctx, &mcp.CallToolParams{

		Name:      "get_all_temperatures",
		Arguments: map[string]interface{}{},
	},
	)
	require.NoError(t, err, "Failed to call get_all_temperatures tool")
	require.NotNil(t, allTempsResult, "Result should not be nil")
	require.False(t, allTempsResult.IsError, "Result should not be an error")
	require.Len(t, allTempsResult.Content, 1, "Result should have one content item")

	allTempsText, ok := allTempsResult.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Contains(t, allTempsText.Text, "Current temperatures in all rooms", "Response should contain header")
	require.Contains(t, allTempsText.Text, "kitchen:", "Response should contain kitchen temperature")
	require.Contains(t, allTempsText.Text, "bedroom:", "Response should contain bedroom temperature")

	// Close the client
	err = client.Close()
	// Ignore "context canceled" errors which can occur during test cleanup
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Failed to close client: %v", err)
	}
}

func TestClientWithStreamableHTTPCalculator(t *testing.T) {
	// Create the MCP calculator server
	calcServer := &mcptest.MCPCalculator{}
	handler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err, "Failed to get HTTP handler")

	// Create httptest server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Create client configuration pointing to the test server
	cfg := config.UpstreamConfig{
		Name:      "calculator-test",
		Transport: "streamablehttp",
		URL:       testServer.URL + "/mcp",
	}

	// Create and initialize the client
	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client.Start(ctx)
	err = client.WaitForConnection(ctx)
	require.NoError(t, err, "Failed to initialize client")
	require.NotNil(t, client.initResult, "Init result should not be nil")
	require.Equal(t, "Calculator Demo", client.initResult.ServerInfo.Name)

	// Test addition
	result, err := client.CallTool(ctx, &mcp.CallToolParams{

		Name: "calculate",
		Arguments: map[string]interface{}{
			"operation": "add",
			"x":         10,
			"y":         15,
		},
	},
	)
	require.NoError(t, err, "Failed to call calculate tool")
	require.NotNil(t, result, "Result should not be nil")
	require.False(t, result.IsError, "Result should not be an error")
	require.Len(t, result.Content, 1, "Result should have one content item")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "25.00", textContent.Text, "Addition result should be 25.00")

	// Test subtraction
	result, err = client.CallTool(ctx, &mcp.CallToolParams{

		Name: "calculate",
		Arguments: map[string]interface{}{
			"operation": "subtract",
			"x":         20,
			"y":         8,
		},
	},
	)
	require.NoError(t, err, "Failed to call calculate tool")
	require.False(t, result.IsError, "Result should not be an error")
	textContent, ok = result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "12.00", textContent.Text, "Subtraction result should be 12.00")

	// Test multiplication
	result, err = client.CallTool(ctx, &mcp.CallToolParams{

		Name: "calculate",
		Arguments: map[string]interface{}{
			"operation": "multiply",
			"x":         6,
			"y":         7,
		},
	},
	)
	require.NoError(t, err, "Failed to call calculate tool")
	require.False(t, result.IsError, "Result should not be an error")
	textContent, ok = result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "42.00", textContent.Text, "Multiplication result should be 42.00")

	// Test division
	result, err = client.CallTool(ctx, &mcp.CallToolParams{

		Name: "calculate",
		Arguments: map[string]interface{}{
			"operation": "divide",
			"x":         100,
			"y":         4,
		},
	},
	)
	require.NoError(t, err, "Failed to call calculate tool")
	require.False(t, result.IsError, "Result should not be an error")
	textContent, ok = result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "25.00", textContent.Text, "Division result should be 25.00")

	// Test division by zero (should return error result)
	result, err = client.CallTool(ctx, &mcp.CallToolParams{

		Name: "calculate",
		Arguments: map[string]interface{}{
			"operation": "divide",
			"x":         10,
			"y":         0,
		},
	},
	)
	require.NoError(t, err, "Call should not return error even for division by zero")
	require.NotNil(t, result, "Result should not be nil")
	require.True(t, result.IsError, "Result should be an error for division by zero")
	textContent, ok = result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Contains(t, textContent.Text, "cannot divide by zero", "Error message should mention division by zero")

	// Close the client
	err = client.Close()
	// Ignore "context canceled" errors which can occur during test cleanup
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Failed to close client: %v", err)
	}
}

func TestClientWithStdioServer(t *testing.T) {
	// Create client configuration for stdio hello server
	// The command will run the hello MCP server via stdio
	cfg := config.UpstreamConfig{
		Name:      "hello-test",
		Transport: "stdio",
		Cmd:       "go",
		CmdArgs: []string{
			"run",
			"../../test/mcp_servers/main.go",
			"server",
			"hello",
		},
	}

	// Create and initialize the client
	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client.Start(ctx)
	err := client.WaitForConnection(ctx)
	require.NoError(t, err, "Failed to initialize client")
	require.NotNil(t, client.initResult, "Init result should not be nil")
	require.Equal(t, "Demo 🚀", client.initResult.ServerInfo.Name)

	// Test calling the hello_world tool
	result, err := client.CallTool(ctx, &mcp.CallToolParams{

		Name: "hello_world",
		Arguments: map[string]interface{}{
			"name": "Alice",
		},
	},
	)
	require.NoError(t, err, "Failed to call hello_world tool")
	require.NotNil(t, result, "Result should not be nil")
	require.False(t, result.IsError, "Result should not be an error")
	require.Len(t, result.Content, 1, "Result should have one content item")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "Hello, Alice!", textContent.Text, "Greeting should match")

	// Test with a different name
	result, err = client.CallTool(ctx, &mcp.CallToolParams{

		Name: "hello_world",
		Arguments: map[string]interface{}{
			"name": "Bob",
		},
	},
	)
	require.NoError(t, err, "Failed to call hello_world tool")
	require.False(t, result.IsError, "Result should not be an error")
	textContent, ok = result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "Hello, Bob!", textContent.Text, "Greeting should match")

	// Close the client
	err = client.Close()
	// Ignore "context canceled" errors which can occur during test cleanup
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Failed to close client: %v", err)
	}
}

func TestClientWithBearerToken_StreamableHTTP(t *testing.T) {
	expectedToken := "test-Bearer-token-456"
	var receivedToken string
	var mu sync.Mutex

	// Create a custom handler that checks for the Authorization header
	tempServer := &mcptest.MCPTemperature{}
	baseHandler, err := tempServer.GetHTTPHandler()
	require.NoError(t, err, "Failed to get HTTP handler")

	// Wrap the handler to capture the Authorization header
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			mu.Lock()
			receivedToken = authHeader
			mu.Unlock()
		}
		baseHandler.ServeHTTP(w, r)
	})

	// Create httptest server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Create client configuration with Bearer token
	cfg := config.UpstreamConfig{
		Name:      "temperature-Bearer-test",
		Transport: "streamablehttp",
		URL:       testServer.URL + "/mcp",
		Bearer:    expectedToken,
	}

	// Create and initialize the client
	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client.Start(ctx)
	err = client.WaitForConnection(ctx)
	require.NoError(t, err, "Failed to initialize client")
	require.NotNil(t, client.initResult, "Init result should not be nil")

	// Verify the Bearer token was sent
	mu.Lock()
	token := receivedToken
	mu.Unlock()
	require.Equal(t, "Bearer "+expectedToken, token, "Authorization header should contain Bearer token")

	// Test that the client can successfully call tools
	result, err := client.CallTool(ctx, &mcp.CallToolParams{

		Name: "get_room_temperature",
		Arguments: map[string]interface{}{
			"room": "kitchen",
		},
	},
	)
	require.NoError(t, err, "Failed to call get_room_temperature tool")
	require.NotNil(t, result, "Result should not be nil")
	require.False(t, result.IsError, "Result should not be an error")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Contains(t, textContent.Text, "temperature in the kitchen", "Response should contain temperature")

	// Close the client
	err = client.Close()
	// Ignore "context canceled" errors which can occur during test cleanup
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Failed to close client: %v", err)
	}
}

func TestClientWithoutBearerToken(t *testing.T) {
	// Verify that no Authorization header is sent when Bearer token is not configured
	receivedAuthHeader := false

	// Create a custom handler that checks for the Authorization header
	calcServer := &mcptest.MCPCalculator{}
	baseHandler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err, "Failed to get HTTP handler")

	// Wrap the handler to check if Authorization header exists
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			receivedAuthHeader = true
		}
		baseHandler.ServeHTTP(w, r)
	})

	// Create httptest server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Create client configuration WITHOUT Bearer token
	cfg := config.UpstreamConfig{
		Name:      "calculator-no-Bearer-test",
		Transport: "streamablehttp",
		URL:       testServer.URL + "/mcp",
		// Bearer is not set
	}

	// Create and initialize the client
	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client.Start(ctx)
	err = client.WaitForConnection(ctx)
	require.NoError(t, err, "Failed to initialize client")

	// Verify no Authorization header was sent
	require.False(t, receivedAuthHeader, "Authorization header should not be sent when Bearer token is not configured")

	// Close the client
	err = client.Close()
	// Ignore "context canceled" errors which can occur during test cleanup
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Failed to close client: %v", err)
	}
}

func TestClientToolsListChangedEvent(t *testing.T) {
	// Create a test MCP server that can send notifications
	calcServer := &mcptest.MCPCalculator{}
	handler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err, "Failed to get HTTP handler")

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Create client configuration
	cfg := config.UpstreamConfig{
		Name:      "notification-test",
		Transport: "streamablehttp",
		URL:       testServer.URL + "/mcp",
	}

	// Create and start the client
	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client.Start(ctx)
	err = client.WaitForConnection(ctx)
	require.NoError(t, err, "Failed to connect client")
	defer client.Close()

	// Wait a bit to ensure the client is fully initialized
	time.Sleep(100 * time.Millisecond)

	// Verify initial tool is "calculate"
	tools, err := client.ListTools(ctx, &mcp.ListToolsParams{})
	require.NoError(t, err, "Failed to list tools")
	require.Len(t, tools.Tools, 1, "Should have exactly one tool")
	require.Equal(t, "calculate", tools.Tools[0].Name, "Initial tool should be 'calculate'")

	// Create a goroutine to wait for the tools list changed event
	eventReceived := make(chan *ClientEvent, 1)
	go func() {
		event, err := client.WaitForEvent(ctx, EventToolsListChanged)
		if err == nil && event != nil {
			eventReceived <- event
		}
	}()

	// Rename the tool from "calculate" to "count" which triggers a notification
	err = calcServer.RenameCalculateToCount(ctx)
	require.NoError(t, err, "Failed to rename tool")

	// Wait for the event with timeout
	select {
	case event := <-eventReceived:
		require.NotNil(t, event, "Event should not be nil")
		require.Equal(t, EventToolsListChanged, event.Type, "Should receive EventToolsListChanged")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for EventToolsListChanged")
	}

	// Verify the last event is EventToolsListChanged
	lastEvent := client.GetLastEvent()
	require.NotNil(t, lastEvent, "Last event should not be nil")
	require.Equal(t, EventToolsListChanged, lastEvent.Type, "Last event should be EventToolsListChanged")

	// Verify the tool is now "count"
	tools, err = client.ListTools(ctx, &mcp.ListToolsParams{})
	require.NoError(t, err, "Failed to list tools after rename")
	require.Len(t, tools.Tools, 1, "Should still have exactly one tool")
	require.Equal(t, "count", tools.Tools[0].Name, "Tool should now be 'count'")
}
