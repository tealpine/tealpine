package proxy_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"mcp-auth-proxy/pkg/proxy"
	"mcp-auth-proxy/test"
)

func TestServerWithInvalidProxyConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create the MCP calculator server
	calcServer := &test.MCPCalculator{}
	calcHandler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err)
	calcUpstreamServer := httptest.NewServer(calcHandler)
	defer calcUpstreamServer.Close()

	// Create configuration with invalid proxy config (references non-existent MCP)
	config := &proxy.Config{
		Server: proxy.ServerConfig{
			Host: "localhost:0",
		},
		MCP: map[string]proxy.MCPConfig{
			"calculator": {
				Name:      "calculator",
				Transport: "sse",
				URL:       calcUpstreamServer.URL + "/sse",
			},
		},
		Proxy: map[string]proxy.ProxyConfig{
			"invalid": {
				Name:      "invalid",
				Path:      "invalid",
				Transport: "streamablehttp",
				MCP:       "nonexistent", // References non-existent MCP
			},
		},
	}

	// Create server
	server := proxy.NewServer(config)

	// Attempt to initialize - should fail because proxy references non-existent MCP
	err = server.Init(ctx)
	require.Error(t, err, "Init should return an error when proxy references non-existent MCP")
	require.Contains(t, err.Error(), "client not found", "Error should indicate client not found")
}

func TestServerWithEmptyProxyConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create the MCP calculator server
	calcServer := &test.MCPCalculator{}
	calcHandler, err := calcServer.GetHTTPHandler()
	require.NoError(t, err)
	calcUpstreamServer := httptest.NewServer(calcHandler)
	defer calcUpstreamServer.Close()

	// Create configuration with empty proxy config (no MCP or MCPs specified)
	config := &proxy.Config{
		Server: proxy.ServerConfig{
			Host: "localhost:0",
		},
		MCP: map[string]proxy.MCPConfig{
			"calculator": {
				Name:      "calculator",
				Transport: "sse",
				URL:       calcUpstreamServer.URL + "/sse",
			},
		},
		Proxy: map[string]proxy.ProxyConfig{
			"empty": {
				Name:      "empty",
				Path:      "empty",
				Transport: "streamablehttp",
				// No MCP or MCPs specified
			},
		},
	}

	// Create server
	server := proxy.NewServer(config)

	// Attempt to initialize - should fail because proxy has no MCP configuration
	err = server.Init(ctx)
	require.Error(t, err, "Init should return an error when proxy has no MCP configuration")
	require.Contains(t, err.Error(), "has no mcp or mcps configuration", "Error should indicate missing MCP configuration")
}
