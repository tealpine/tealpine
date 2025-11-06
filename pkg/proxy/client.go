package proxy

import (
	"context"
	"fmt"
	mgclient "github.com/mark3labs/mcp-go/client"
	mgmcp "github.com/mark3labs/mcp-go/mcp"
	"log"
)

type Client struct {
	cfg        MCPConfig
	client     mgclient.MCPClient
	initResult *mgmcp.InitializeResult
}

func NewClient(cfg MCPConfig) *Client {
	s := &Client{
		cfg: cfg,
	}
	return s
}

func (cs *Client) Init(ctx context.Context) (*mgmcp.InitializeResult, error) {
	var err error
	var client mgclient.MCPClient
	switch cs.cfg.Transport {
	case "sse":
		client, err = mgclient.NewSSEMCPClient(cs.cfg.URL)
	case "streamablehttp":
		client, err = mgclient.NewStreamableHttpClient(cs.cfg.URL)
	case "stdio":
		client, err = mgclient.NewStdioMCPClient(cs.cfg.Cmd, []string{}, cs.cfg.CmdArgs...)
	}

	if err != nil {
		return nil, err
	}
	cs.client = client

	if cs.cfg.Transport != "stdio" {
		// we need to do type assertion to call Start method
		type starter interface {
			Start(context.Context) error
		}
		if s, ok := cs.client.(starter); ok {
			err = s.Start(ctx)
			if err != nil {
				return nil, err
			}
		}
	}

	result, err := cs.client.Initialize(ctx, mgmcp.InitializeRequest{
		Params: mgmcp.InitializeParams{
			ProtocolVersion: mgmcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mgmcp.Implementation{
				Name:    "mcp-auth-proxy-upstream-client",
				Version: "1.0.0",
			},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("initialization failed: %w", err)
	}

	log.Printf("Connected to server: %s v%s",
		result.ServerInfo.Name,
		result.ServerInfo.Version)

	log.Printf("Client capabilities: %+v", result.Capabilities)

	cs.initResult = result

	return result, nil

}

func (cs *Client) CallTool(
	ctx context.Context,
	request mgmcp.CallToolRequest,
) (*mgmcp.CallToolResult, error) {
	return cs.client.CallTool(ctx, request)
}

func (cs *Client) ListTools(ctx context.Context, request mgmcp.ListToolsRequest) (*mgmcp.ListToolsResult, error) {
	return cs.client.ListTools(ctx, request)
}

func (cs *Client) ListResources(ctx context.Context, request mgmcp.ListResourcesRequest) (*mgmcp.ListResourcesResult, error) {
	return cs.client.ListResources(ctx, request)
}

func (cs *Client) ReadResource(ctx context.Context, request mgmcp.ReadResourceRequest) (*mgmcp.ReadResourceResult, error) {
	return cs.client.ReadResource(ctx, request)
}

func (cs *Client) ListResourceTemplates(ctx context.Context, request mgmcp.ListResourceTemplatesRequest) (*mgmcp.ListResourceTemplatesResult, error) {
	return cs.client.ListResourceTemplates(ctx, request)
}

func (cs *Client) GetPrompt(ctx context.Context, request mgmcp.GetPromptRequest) (*mgmcp.GetPromptResult, error) {
	return cs.client.GetPrompt(ctx, request)
}

func (cs *Client) ListPrompts(ctx context.Context, request mgmcp.ListPromptsRequest) (*mgmcp.ListPromptsResult, error) {
	return cs.client.ListPrompts(ctx, request)
}

func (cs *Client) Close() error {
	return cs.client.Close()
}
