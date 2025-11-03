package test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"mcp-auth-proxy/pkg/mcp_proxy"
)

func TestProxyServer(t *testing.T) {
	config := getConfig(t)
	fmt.Printf("config: %+v\n", config)
	server := mcp_proxy.NewServer(t.Context(), config)

	mcps := server.ListMCP()
	sort.Strings(mcps)
	require.Equal(t, []string{"calculator", "hello", "temperature"}, mcps)

	result, err := server.CallTool(t.Context(), "calculator", mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": "add",
				"x":         10,
				"y":         15,
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "25.00", result.Content[0].(mcp.TextContent).Text)

	result, err = server.CallTool(t.Context(), "temperature", mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "bedroom",
			},
		},
	})
	require.NoError(t, err)
	require.Contains(t, result.Content[0].(mcp.TextContent).Text, "The temperature in the bedroom is")

	result, err = server.CallTool(t.Context(), "hello", mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "hello_world",
			Arguments: map[string]interface{}{
				"name": "John",
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Hello, John!", result.Content[0].(mcp.TextContent).Text)

	err = server.Shutdown()
	require.NoError(t, err)
}
