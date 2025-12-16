package server_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"mcp-auth-proxy/pkg/config"
	"mcp-auth-proxy/pkg/server"
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
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost:0",
		},
		MCP: map[string]config.MCPConfig{
			"calculator": {
				Name:      "calculator",
				Transport: "streamablehttp",
				URL:       calcUpstreamServer.URL + "/mcp",
			},
		},
		Proxy: map[string]config.ProxyConfig{
			"invalid": {
				Name:      "invalid",
				Path:      "invalid",
				Transport: "streamablehttp",
				MCP:       "nonexistent", // References non-existent MCP
			},
		},
	}

	// Create server
	s := server.NewServer(cfg)

	// Attempt to initialize - should fail because proxy references non-existent MCP
	err = s.Init(ctx)
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
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost:0",
		},
		MCP: map[string]config.MCPConfig{
			"calculator": {
				Name:      "calculator",
				Transport: "streamablehttp",
				URL:       calcUpstreamServer.URL + "/mcp",
			},
		},
		Proxy: map[string]config.ProxyConfig{
			"empty": {
				Name:      "empty",
				Path:      "empty",
				Transport: "streamablehttp",
				// No MCP or MCPs specified
			},
		},
	}

	// Create server
	s := server.NewServer(cfg)

	// Attempt to initialize - should fail because proxy has no MCP configuration
	err = s.Init(ctx)
	require.Error(t, err, "Init should return an error when proxy has no MCP configuration")
	require.Contains(t, err.Error(), "has no mcp or mcps configuration", "Error should indicate missing MCP configuration")
}
