package mcp_proxy

import (
	"context"
	"errors"
	"fmt"
	mcpclint "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"log"
)

type Server struct {
	cfg     Config
	clients map[string]*mcpclint.Client
}

func NewServer(ctx context.Context, cfg *Config) *Server {
	s := &Server{
		cfg: *cfg,
	}
	s.init(ctx)
	return s
}

func (s *Server) ListMCP() []string {
	var mcps []string
	for c := range s.clients {
		mcps = append(mcps, c)
	}
	return mcps
}

func (s *Server) CallTool(
	ctx context.Context,
	name string,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return s.clients[name].CallTool(ctx, request)
}

func (s *Server) Shutdown() error {
	var finalErr error
	for _, client := range s.clients {
		if err := client.Close(); err != nil {
			errors.Join(finalErr, err)
		}
	}
	return finalErr
}

func (s *Server) init(ctx context.Context) {
	clients := make(map[string]*mcpclint.Client)
	for m := range s.cfg.MCP {
		mcpCfg := s.cfg.MCP[m]
		var mcpCli *mcpclint.Client
		var err error
		switch mcpCfg.Transport {
		case "sse":
			mcpCli, err = mcpclint.NewSSEMCPClient(mcpCfg.URL)
			if err == nil {
				err = mcpCli.Start(ctx)
			}
		case "streamablehttp":
			mcpCli, err = mcpclint.NewStreamableHttpClient(mcpCfg.URL)
			if err == nil {
				err = mcpCli.Start(ctx)
			}
		case "stdio":
			mcpCli, err = mcpclint.NewStdioMCPClient(mcpCfg.Cmd, []string{}, mcpCfg.CmdArgs...)
		}
		if mcpCli != nil && err == nil {
			err = initializeClient(ctx, mcpCli)
		}

		switch err {
		case nil:
			clients[mcpCfg.Name] = mcpCli
		default:
			log.Printf("Failed to create client: %s %v", mcpCfg.Name, err)
		}
	}

	s.clients = clients
}

func initializeClient(ctx context.Context, c *mcpclint.Client) error {
	result, err := c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "temperature-client",
				Version: "1.0.0",
			},
		},
	})

	if err != nil {
		return fmt.Errorf("initialization failed: %w", err)
	}

	log.Printf("Connected to server: %s v%s",
		result.ServerInfo.Name,
		result.ServerInfo.Version)

	log.Printf("Server capabilities: %+v", result.Capabilities)

	return nil
}
