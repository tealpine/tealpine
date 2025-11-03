package test

import (
	"context"
	"testing"
	"time"

	mgclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"mcp-auth-proxy/pkg/mcp_proxy"
)

func TestServer(t *testing.T) {
	config := getConfig(t)
	server := mcp_proxy.NewServer(config)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	err := server.Init(ctx)
	require.NoError(t, err)

	go func() {
		if err := server.Run(); err != nil {
			t.Logf("server.Run() returned an error: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond) // give server time to start

	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		server.Stop(stopCtx)
	}()

	// Test "calc" proxy (streamablehttp)
	t.Run("SingleProxy_Calculator_StreamableHTTP", func(t *testing.T) {
		calcClient, err := mgclient.NewStreamableHttpClient("http://" + config.Server.Host + "/calc/mcp")
		require.NoError(t, err)
		err = calcClient.Start(ctx)
		require.NoError(t, err)
		defer calcClient.Close()
		_, err = calcClient.Initialize(ctx, mcp.InitializeRequest{})
		require.NoError(t, err)
		result, err := calcClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "calculate",
				Arguments: map[string]interface{}{"operation": "add", "x": 1, "y": 2},
			},
		})
		require.NoError(t, err)
		require.Equal(t, "3.00", result.Content[0].(mcp.TextContent).Text)
	})

	// Test "temp" proxy (sse)
	t.Run("SingleProxy_Temperature_SSE", func(t *testing.T) {
		tempClient, err := mgclient.NewSSEMCPClient("http://" + config.Server.Host + "/temp/sse")
		require.NoError(t, err)
		err = tempClient.Start(ctx)
		require.NoError(t, err)
		defer tempClient.Close()
		_, err = tempClient.Initialize(ctx, mcp.InitializeRequest{})
		require.NoError(t, err)
		result, err := tempClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_room_temperature",
				Arguments: map[string]interface{}{"room": "kitchen"},
			},
		})
		require.NoError(t, err)
		require.Contains(t, result.Content[0].(mcp.TextContent).Text, "The temperature in the kitchen is")
	})

	// Test "hello" proxy (sse)
	t.Run("SingleProxy_Hello_SSE", func(t *testing.T) {
		helloClient, err := mgclient.NewSSEMCPClient("http://" + config.Server.Host + "/hello/sse")
		require.NoError(t, err)
		err = helloClient.Start(ctx)
		require.NoError(t, err)
		defer helloClient.Close()
		_, err = helloClient.Initialize(ctx, mcp.InitializeRequest{})
		require.NoError(t, err)
		result, err := helloClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "hello_world",
				Arguments: map[string]interface{}{"name": "Server"},
			},
		})
		require.NoError(t, err)
		require.Equal(t, "Hello, Server!", result.Content[0].(mcp.TextContent).Text)
	})

	// Test "multi" proxy (streamablehttp)
	t.Run("MultiProxy_All_StreamableHTTP", func(t *testing.T) {
		multiClient, err := mgclient.NewStreamableHttpClient("http://" + config.Server.Host + "/multi/mcp")
		require.NoError(t, err)
		err = multiClient.Start(ctx)
		require.NoError(t, err)
		defer multiClient.Close()
		_, err = multiClient.Initialize(ctx, mcp.InitializeRequest{})
		require.NoError(t, err)

		// Calc
		result, err := multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "calc_calculate",
				Arguments: map[string]interface{}{"operation": "subtract", "x": 10, "y": 3},
			},
		})
		require.NoError(t, err)
		require.Equal(t, "7.00", result.Content[0].(mcp.TextContent).Text)

		// Temp
		result, err = multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "temp_get_room_temperature",
				Arguments: map[string]interface{}{"room": "bedroom"},
			},
		})
		require.NoError(t, err)
		require.Contains(t, result.Content[0].(mcp.TextContent).Text, "The temperature in the bedroom is")

		// Hello
		result, err = multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "hello_hello_world",
				Arguments: map[string]interface{}{"name": "Final Test"},
			},
		})
		require.NoError(t, err)
		require.Equal(t, "Hello, Final Test!", result.Content[0].(mcp.TextContent).Text)
	})

	// Test "multisse" proxy (sse)
	t.Run("MultiProxy_All_SSE", func(t *testing.T) {
		multiClient, err := mgclient.NewSSEMCPClient("http://" + config.Server.Host + "/multisse/sse")
		require.NoError(t, err)
		err = multiClient.Start(ctx)
		require.NoError(t, err)
		defer multiClient.Close()
		_, err = multiClient.Initialize(ctx, mcp.InitializeRequest{})
		require.NoError(t, err)

		// Calc
		result, err := multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "calc_calculate",
				Arguments: map[string]interface{}{"operation": "subtract", "x": 10, "y": 3},
			},
		})
		require.NoError(t, err)
		require.Equal(t, "7.00", result.Content[0].(mcp.TextContent).Text)

		// Temp
		result, err = multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "temp_get_room_temperature",
				Arguments: map[string]interface{}{"room": "bedroom"},
			},
		})
		require.NoError(t, err)
		require.Contains(t, result.Content[0].(mcp.TextContent).Text, "The temperature in the bedroom is")

		// Hello
		result, err = multiClient.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "hello_hello_world",
				Arguments: map[string]interface{}{"name": "Final Test"},
			},
		})
		require.NoError(t, err)
		require.Equal(t, "Hello, Final Test!", result.Content[0].(mcp.TextContent).Text)
	})
}
