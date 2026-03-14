package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"tealpine/pkg/config"
	"tealpine/pkg/server"
	"tealpine/test"
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

	// Create configuration with invalid mcp config (references non-existent upstream)
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost:0",
		},
		Upstream: map[string]config.UpstreamConfig{
			"calculator": {
				Name:      "calculator",
				Transport: "streamablehttp",
				URL:       calcUpstreamServer.URL + "/mcp",
			},
		},
		MCPs: map[string]config.MCPConfig{
			"invalid": {
				Name:      "invalid",
				Path:      "invalid",
				Transport: "streamablehttp",
				Upstream:  "nonexistent", // References non-existent upstream
			},
		},
	}

	// Create server
	s := server.NewServer(cfg, "")

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

	// Create configuration with empty mcp config (no upstream or upstreams specified)
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost:0",
		},
		Upstream: map[string]config.UpstreamConfig{
			"calculator": {
				Name:      "calculator",
				Transport: "streamablehttp",
				URL:       calcUpstreamServer.URL + "/mcp",
			},
		},
		MCPs: map[string]config.MCPConfig{
			"empty": {
				Name:      "empty",
				Path:      "empty",
				Transport: "streamablehttp",
				// No upstream or upstreams specified
			},
		},
	}

	// Create server
	s := server.NewServer(cfg, "")

	// Attempt to initialize - should fail because proxy has no MCP configuration
	err = s.Init(ctx)
	require.Error(t, err, "Init should return an error when proxy has no MCP configuration")
	require.Contains(t, err.Error(), "has no upstream or upstreams configuration", "Error should indicate missing upstream configuration")
}

func TestStatusEndpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Create two MCP calculator servers
	calcServer1 := &test.MCPCalculator{}
	calcHandler1, err := calcServer1.GetHTTPHandler()
	require.NoError(t, err)
	calcUpstreamServer1 := httptest.NewServer(calcHandler1)
	defer calcUpstreamServer1.Close()

	calcServer2 := &test.MCPCalculator{}
	calcHandler2, err := calcServer2.GetHTTPHandler()
	require.NoError(t, err)
	calcUpstreamServer2 := httptest.NewServer(calcHandler2)
	defer calcUpstreamServer2.Close()

	// Get a free port for the proxy server
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	serverAddr := listener.Addr().String()
	listener.Close()

	// Create configuration with two upstream clients
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:  serverAddr,
			Admin: []string{"admin-group"},
		},
		Upstream: map[string]config.UpstreamConfig{
			"calculator1": {
				Name:         "calculator1",
				Transport:    "streamablehttp",
				URL:          calcUpstreamServer1.URL + "/mcp",
				PingInterval: 2 * time.Second, // Short interval for faster test
			},
			"calculator2": {
				Name:         "calculator2",
				Transport:    "streamablehttp",
				URL:          calcUpstreamServer2.URL + "/mcp",
				PingInterval: 2 * time.Second, // Short interval for faster test
			},
		},
		MCPs: map[string]config.MCPConfig{
			"calc1": {
				Name:      "calc1",
				Path:      "calc1",
				Transport: "streamablehttp",
				Upstream:  "calculator1",
			},
			"calc2": {
				Name:      "calc2",
				Path:      "calc2",
				Transport: "streamablehttp",
				Upstream:  "calculator2",
			},
		},
		Groups: map[string][]string{
			"admin-group": {"admin"},
		},
		Users: map[string]config.UserConfig{
			"admin": {
				Token: "admin-secret-token",
			},
		},
	}

	// Create and initialize server
	s := server.NewServer(cfg, "")
	err = s.Init(ctx)
	require.NoError(t, err)

	// Wait for both clients to connect
	err = s.WaitForClients(ctx)
	require.NoError(t, err)

	// Start the server in a goroutine
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- s.Run()
	}()
	defer func() {
		_ = s.Stop(ctx)
		<-serverErrCh
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Test 1: Check both clients are connected
	req, err := http.NewRequest("GET", "http://"+serverAddr+"/tealpine/api/v1/status", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer admin-secret-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)

	var status1 struct {
		Clients []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"clients"`
	}
	err = json.Unmarshal(body, &status1)
	require.NoError(t, err)
	require.Len(t, status1.Clients, 2, "Should have 2 clients")

	// Verify both clients are connected
	connectedCount := 0
	for _, client := range status1.Clients {
		if client.Status == "connected" {
			connectedCount++
		}
	}
	require.Equal(t, 2, connectedCount, "Both clients should be connected")

	// Test 2: Shutdown one MCP server
	calcUpstreamServer1.Close()

	// Wait for the client to detect disconnection (ping interval + ping timeout + buffer)
	// Ping interval is 2s, ping timeout is 5s, so wait 8s to be safe
	time.Sleep(8 * time.Second)

	// Check status again
	req, err = http.NewRequest("GET", "http://"+serverAddr+"/tealpine/api/v1/status", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer admin-secret-token")

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)

	var status2 struct {
		Clients []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"clients"`
	}
	err = json.Unmarshal(body, &status2)
	require.NoError(t, err)
	require.Len(t, status2.Clients, 2, "Should still have 2 clients")

	// Verify one client is disconnected
	connectedCount = 0
	disconnectedCount := 0
	for _, client := range status2.Clients {
		if client.Status == "connected" {
			connectedCount++
		} else if client.Status == "disconnected" {
			disconnectedCount++
		}
	}
	require.Equal(t, 1, connectedCount, "One client should be connected")
	require.Equal(t, 1, disconnectedCount, "One client should be disconnected")
}
