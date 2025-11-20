package proxy

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	mcptest "mcp-auth-proxy/test"
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
	config := MCPConfig{
		Name:      "temperature-test",
		Transport: "streamablehttp",
		URL:       testServer.URL + "/mcp",
	}

	// Create and initialize the client
	client := NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client.Start(ctx)

	err = client.WaitForConnection(ctx)
	require.NoError(t, err, "Failed to initialize client")
	require.NotNil(t, client.initResult, "Init result should not be nil")
	require.Equal(t, "apartment-temperature-server", client.initResult.ServerInfo.Name)

	// Test calling the get_room_temperature tool
	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "bedroom",
			},
		},
	})
	require.NoError(t, err, "Failed to call get_room_temperature tool")
	require.NotNil(t, result, "Result should not be nil")
	require.False(t, result.IsError, "Result should not be an error")
	require.Len(t, result.Content, 1, "Result should have one content item")

	// Check that the response contains expected text
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Contains(t, textContent.Text, "The temperature in the bedroom is", "Response should contain temperature message")
	require.Contains(t, textContent.Text, "°C", "Response should contain temperature unit")

	// Test calling with an invalid room
	invalidResult, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "garage",
			},
		},
	})
	require.NoError(t, err, "Call should not return error even for invalid room")
	require.NotNil(t, invalidResult, "Result should not be nil")
	require.True(t, invalidResult.IsError, "Result should be an error for invalid room")

	// Test calling get_all_temperatures tool
	allTempsResult, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "get_all_temperatures",
			Arguments: map[string]interface{}{},
		},
	})
	require.NoError(t, err, "Failed to call get_all_temperatures tool")
	require.NotNil(t, allTempsResult, "Result should not be nil")
	require.False(t, allTempsResult.IsError, "Result should not be an error")
	require.Len(t, allTempsResult.Content, 1, "Result should have one content item")

	allTempsText, ok := allTempsResult.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Contains(t, allTempsText.Text, "Current temperatures in all rooms", "Response should contain header")
	require.Contains(t, allTempsText.Text, "kitchen:", "Response should contain kitchen temperature")
	require.Contains(t, allTempsText.Text, "bedroom:", "Response should contain bedroom temperature")

	// Close the client
	err = client.Close()
	require.NoError(t, err, "Failed to close client")
}

func TestClientWithSSEServer(t *testing.T) {
	// Create the MCP calculator server
	calcServer := &mcptest.MCPCalculator{}
	handler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err, "Failed to get HTTP handler")

	// Create httptest server
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Create client configuration pointing to the test server
	config := MCPConfig{
		Name:      "calculator-test",
		Transport: "sse",
		URL:       testServer.URL + "/sse",
	}

	// Create and initialize the client
	client := NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client.Start(ctx)
	err = client.WaitForConnection(ctx)
	require.NoError(t, err, "Failed to initialize client")
	require.NotNil(t, client.initResult, "Init result should not be nil")
	require.Equal(t, "Calculator Demo", client.initResult.ServerInfo.Name)

	// Test addition
	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "add",
				"x":         10,
				"y":         15,
			},
		},
	})
	require.NoError(t, err, "Failed to call calculate tool")
	require.NotNil(t, result, "Result should not be nil")
	require.False(t, result.IsError, "Result should not be an error")
	require.Len(t, result.Content, 1, "Result should have one content item")

	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "25.00", textContent.Text, "Addition result should be 25.00")

	// Test subtraction
	result, err = client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "subtract",
				"x":         20,
				"y":         8,
			},
		},
	})
	require.NoError(t, err, "Failed to call calculate tool")
	require.False(t, result.IsError, "Result should not be an error")
	textContent, ok = result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "12.00", textContent.Text, "Subtraction result should be 12.00")

	// Test multiplication
	result, err = client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "multiply",
				"x":         6,
				"y":         7,
			},
		},
	})
	require.NoError(t, err, "Failed to call calculate tool")
	require.False(t, result.IsError, "Result should not be an error")
	textContent, ok = result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "42.00", textContent.Text, "Multiplication result should be 42.00")

	// Test division
	result, err = client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "divide",
				"x":         100,
				"y":         4,
			},
		},
	})
	require.NoError(t, err, "Failed to call calculate tool")
	require.False(t, result.IsError, "Result should not be an error")
	textContent, ok = result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "25.00", textContent.Text, "Division result should be 25.00")

	// Test division by zero (should return error result)
	result, err = client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "divide",
				"x":         10,
				"y":         0,
			},
		},
	})
	require.NoError(t, err, "Call should not return error even for division by zero")
	require.NotNil(t, result, "Result should not be nil")
	require.True(t, result.IsError, "Result should be an error for division by zero")
	textContent, ok = result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Contains(t, textContent.Text, "cannot divide by zero", "Error message should mention division by zero")

	// Close the client
	err = client.Close()
	require.NoError(t, err, "Failed to close client")
}

func TestClientWithStdioServer(t *testing.T) {
	// Create client configuration for stdio hello server
	// The command will run the hello MCP server via stdio
	config := MCPConfig{
		Name:      "hello-test",
		Transport: "stdio",
		Cmd:       "go",
		CmdArgs: []string{
			"run",
			"../../test/mcp_servers/main.go",
			"hello",
			"server",
		},
	}

	// Create and initialize the client
	client := NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client.Start(ctx)
	err := client.WaitForConnection(ctx)
	require.NoError(t, err, "Failed to initialize client")
	require.NotNil(t, client.initResult, "Init result should not be nil")
	require.Equal(t, "Demo 🚀", client.initResult.ServerInfo.Name)

	// Test calling the hello_world tool
	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "hello_world",
			Arguments: map[string]interface{}{
				"name": "Alice",
			},
		},
	})
	require.NoError(t, err, "Failed to call hello_world tool")
	require.NotNil(t, result, "Result should not be nil")
	require.False(t, result.IsError, "Result should not be an error")
	require.Len(t, result.Content, 1, "Result should have one content item")

	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "Hello, Alice!", textContent.Text, "Greeting should match")

	// Test with a different name
	result, err = client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "hello_world",
			Arguments: map[string]interface{}{
				"name": "Bob",
			},
		},
	})
	require.NoError(t, err, "Failed to call hello_world tool")
	require.False(t, result.IsError, "Result should not be an error")
	textContent, ok = result.Content[0].(mcp.TextContent)
	require.True(t, ok, "Content should be TextContent")
	require.Equal(t, "Hello, Bob!", textContent.Text, "Greeting should match")

	// Close the client
	err = client.Close()
	require.NoError(t, err, "Failed to close client")
}
