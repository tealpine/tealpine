package test

import (
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"mcp-auth-proxy/pkg/mcp_proxy"
)

func TestClient(t *testing.T) {
	config := getConfig(t)
	fmt.Printf("config: %+v\n", config)
	clientTemp := mcp_proxy.NewClient(config.MCP["temperature"])
	clientCalc := mcp_proxy.NewClient(config.MCP["calculator"])
	clientHello := mcp_proxy.NewClient(config.MCP["hello"])

	var err error
	_, err = clientTemp.Init(t.Context())
	require.NoError(t, err)
	_, err = clientCalc.Init(t.Context())
	require.NoError(t, err)
	_, err = clientHello.Init(t.Context())
	require.NoError(t, err)

	result, err := clientCalc.CallTool(t.Context(), mcp.CallToolRequest{
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

	result, err = clientTemp.CallTool(t.Context(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "bedroom",
			},
		},
	})
	require.NoError(t, err)
	require.Contains(t, result.Content[0].(mcp.TextContent).Text, "The temperature in the bedroom is")

	result, err = clientHello.CallTool(t.Context(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "hello_world",
			Arguments: map[string]interface{}{
				"name": "John",
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Hello, John!", result.Content[0].(mcp.TextContent).Text)

	err = clientTemp.Close()
	require.NoError(t, err)

	err = clientHello.Close()
	require.NoError(t, err)

	err = clientCalc.Close()
	require.NoError(t, err)
}
