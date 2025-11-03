package mcp_proxy

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	mgmcp "github.com/mark3labs/mcp-go/mcp"
	mgserver "github.com/mark3labs/mcp-go/server"
)

// SingleProxy acts as a bridge between MCP clients and servers
// It exposes an MCP server (via StreamableHTTP or SSE) and forwards
// all requests to an upstream MCP client
type SingleProxy struct {
	transport   string
	path        string
	client      *Client
	mcpServer   *mgserver.MCPServer
	httpHandler http.Handler
}

// NewSingleProxy creates a new proxy that will expose the given client
// via the specified transport (either "streamablehttp" or "sse")
func NewSingleProxy(transport string, client *Client, path string) *SingleProxy {
	return &SingleProxy{
		transport: transport,
		path:      path,
		client:    client,
	}
}

// Init initializes the proxy by creating an MCP server that forwards
// all requests to the upstream client
func (p *SingleProxy) Init(ctx context.Context) error {
	// Create an MCP server that will proxy requests
	p.mcpServer = mgserver.NewMCPServer(
		"mcp-auth-proxy",
		"1.0.0",
		mgserver.WithInstructions("MCP Authentication Proxy"),
	)

	// Set up handlers that forward to the upstream client
	if err := p.setupProxyHandlers(ctx, p.client.initResult); err != nil {
		return fmt.Errorf("failed to setup proxy handlers: %w", err)
	}

	// Create the appropriate transport server
	switch p.transport {
	case "streamablehttp":
		p.httpHandler = mgserver.NewStreamableHTTPServer(p.mcpServer)
	case "sse":
		p.httpHandler = mgserver.NewSSEServer(p.mcpServer, mgserver.WithStaticBasePath(p.path))
	default:
		return fmt.Errorf("unsupported transport: %s (must be 'streamablehttp' or 'sse')", p.transport)
	}

	return nil
}

// setupProxyHandlers configures the MCP server to forward all requests to the upstream client
func (p *SingleProxy) setupProxyHandlers(ctx context.Context, initResult *mgmcp.InitializeResult) error {
	// Configure server capabilities to match upstream capabilities
	if initResult.Capabilities.Tools != nil {
		// Fetch all tools from upstream
		var allTools []mgmcp.Tool
		toolsRequest := mgmcp.ListToolsRequest{}
		for {
			toolsResult, err := p.client.ListTools(ctx, toolsRequest)
			if err != nil {
				return fmt.Errorf("failed to list tools from upstream: %w", err)
			}
			if len(toolsResult.Tools) == 0 {
				break
			}
			allTools = append(allTools, toolsResult.Tools...)
			if toolsResult.NextCursor == "" {
				break
			}
			toolsRequest.Params.Cursor = toolsResult.NextCursor
		}

		// Register each tool with a proxy handler
		for _, tool := range allTools {
			toolCopy := tool // Capture for closure
			p.mcpServer.AddTool(toolCopy, func(ctx context.Context, request mgmcp.CallToolRequest) (*mgmcp.CallToolResult, error) {
				// Forward the tool call to the upstream client
				return p.client.CallTool(ctx, request)
			})
		}
	}

	if initResult.Capabilities.Resources != nil {
		// Fetch all resources from upstream
		var allResources []mgmcp.Resource
		resourcesRequest := mgmcp.ListResourcesRequest{}
		for {
			resourcesResult, err := p.client.ListResources(ctx, resourcesRequest)
			if err != nil {
				return fmt.Errorf("failed to list resources from upstream: %w", err)
			}
			if len(resourcesResult.Resources) == 0 {
				break
			}
			allResources = append(allResources, resourcesResult.Resources...)
			if resourcesResult.NextCursor == "" {
				break
			}
			resourcesRequest.Params.Cursor = resourcesResult.NextCursor
		}

		// Register each resource with a proxy handler
		for _, resource := range allResources {
			resourceCopy := resource // Capture for closure
			p.mcpServer.AddResource(resourceCopy, func(ctx context.Context, request mgmcp.ReadResourceRequest) ([]mgmcp.ResourceContents, error) {
				// Forward the resource read to the upstream client
				result, err := p.client.ReadResource(ctx, request)
				if err != nil {
					return nil, err
				}
				return result.Contents, nil
			})
		}

		// Fetch all resource templates from upstream
		var allTemplates []mgmcp.ResourceTemplate
		templatesRequest := mgmcp.ListResourceTemplatesRequest{}
		for {
			templatesResult, err := p.client.ListResourceTemplates(ctx, templatesRequest)
			if err != nil {
				return fmt.Errorf("failed to list resource templates from upstream: %w", err)
			}
			if len(templatesResult.ResourceTemplates) == 0 {
				break
			}
			allTemplates = append(allTemplates, templatesResult.ResourceTemplates...)
			if templatesResult.NextCursor == "" {
				break
			}
			templatesRequest.Params.Cursor = templatesResult.NextCursor
		}

		// Register each resource template with a proxy handler
		for _, template := range allTemplates {
			templateCopy := template // Capture for closure
			p.mcpServer.AddResourceTemplate(templateCopy, func(ctx context.Context, request mgmcp.ReadResourceRequest) ([]mgmcp.ResourceContents, error) {
				// Forward the resource template read to the upstream client
				result, err := p.client.ReadResource(ctx, request)
				if err != nil {
					return nil, err
				}
				return result.Contents, nil
			})
		}
	}

	if initResult.Capabilities.Prompts != nil {
		// Fetch all prompts from upstream
		var allPrompts []mgmcp.Prompt
		promptsRequest := mgmcp.ListPromptsRequest{}
		for {
			promptsResult, err := p.client.ListPrompts(ctx, promptsRequest)
			if err != nil {
				return fmt.Errorf("failed to list prompts from upstream: %w", err)
			}
			if len(promptsResult.Prompts) == 0 {
				break
			}
			allPrompts = append(allPrompts, promptsResult.Prompts...)
			if promptsResult.NextCursor == "" {
				break
			}
			promptsRequest.Params.Cursor = promptsResult.NextCursor
		}

		// Register each prompt with a proxy handler
		for _, prompt := range allPrompts {
			promptCopy := prompt // Capture for closure
			p.mcpServer.AddPrompt(promptCopy, func(ctx context.Context, request mgmcp.GetPromptRequest) (*mgmcp.GetPromptResult, error) {
				// Forward the prompt request to the upstream client
				return p.client.GetPrompt(ctx, request)
			})
		}
	}

	return nil
}

// ServeHTTP implements the http.Handler interface, allowing the proxy
// to handle HTTP requests
func (p *SingleProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.httpHandler == nil {
		http.Error(w, "proxy not initialized", http.StatusInternalServerError)
		return
	}
	p.httpHandler.ServeHTTP(w, r)
}

// MultiProxy acts as a bridge between multiple MCP clients and a single server endpoint
type MultiProxy struct {
	transport   string
	path        string
	clients     map[string]*Client
	mcps        []MultiMCPConfig
	mcpServer   *mgserver.MCPServer
	httpHandler http.Handler
}

// NewMultiProxy creates a new multi-proxy
func NewMultiProxy(
	transport string,
	clients map[string]*Client,
	mcps []MultiMCPConfig,
	path string,
) *MultiProxy {
	return &MultiProxy{
		transport: transport,
		path:      path,
		clients:   clients,
		mcps:      mcps,
	}
}

// Init initializes the multi-proxy
func (p *MultiProxy) Init(ctx context.Context) error {
	p.mcpServer = mgserver.NewMCPServer(
		"mcp-auth-multi-proxy",
		"1.0.0",
		mgserver.WithInstructions("MCP Authentication Multi-Proxy"),
	)

	for _, mcpConfig := range p.mcps {
		client, ok := p.clients[mcpConfig.Name]
		if !ok {
			return fmt.Errorf("client not found for mcp config: %s", mcpConfig.Name)
		}

		if err := p.setupProxyHandlers(ctx, client, client.initResult, mcpConfig.Prefix); err != nil {
			return fmt.Errorf("failed to setup proxy handlers for client %s: %w", mcpConfig.Name, err)
		}
	}

	switch p.transport {
	case "streamablehttp":
		p.httpHandler = mgserver.NewStreamableHTTPServer(p.mcpServer)
	case "sse":
		p.httpHandler = mgserver.NewSSEServer(p.mcpServer, mgserver.WithStaticBasePath(p.path))
	default:
		return fmt.Errorf("unsupported transport: %s (must be 'streamablehttp' or 'sse')", p.transport)
	}

	return nil
}

// setupProxyHandlers configures the MCP server to forward all requests to the upstream client
func (p *MultiProxy) setupProxyHandlers(ctx context.Context, client *Client, initResult *mgmcp.InitializeResult, prefix string) error {
	if initResult.Capabilities.Tools != nil {
		var allTools []mgmcp.Tool
		toolsRequest := mgmcp.ListToolsRequest{}
		for {
			toolsResult, err := client.ListTools(ctx, toolsRequest)
			if err != nil {
				return fmt.Errorf("failed to list tools from upstream: %w", err)
			}
			if len(toolsResult.Tools) == 0 {
				break
			}
			allTools = append(allTools, toolsResult.Tools...)
			if toolsResult.NextCursor == "" {
				break
			}
			toolsRequest.Params.Cursor = toolsResult.NextCursor
		}

		for _, tool := range allTools {
			toolCopy := tool
			toolCopy.Name = prefix + "_" + toolCopy.Name
			p.mcpServer.AddTool(toolCopy, func(ctx context.Context, request mgmcp.CallToolRequest) (*mgmcp.CallToolResult, error) {
				request.Params.Name = strings.TrimPrefix(request.Params.Name, prefix+"_")
				return client.CallTool(ctx, request)
			})
		}
	}

	if initResult.Capabilities.Resources != nil {
		var allResources []mgmcp.Resource
		resourcesRequest := mgmcp.ListResourcesRequest{}
		for {
			resourcesResult, err := client.ListResources(ctx, resourcesRequest)
			if err != nil {
				return fmt.Errorf("failed to list resources from upstream: %w", err)
			}
			if len(resourcesResult.Resources) == 0 {
				break
			}
			allResources = append(allResources, resourcesResult.Resources...)
			if resourcesResult.NextCursor == "" {
				break
			}
			resourcesRequest.Params.Cursor = resourcesResult.NextCursor
		}

		for _, resource := range allResources {
			resourceCopy := resource
			resourceCopy.Name = prefix + "_" + resourceCopy.Name
			p.mcpServer.AddResource(resourceCopy, func(ctx context.Context, request mgmcp.ReadResourceRequest) ([]mgmcp.ResourceContents, error) {
				request.Params.URI = strings.TrimPrefix(request.Params.URI, prefix+"_")
				result, err := client.ReadResource(ctx, request)
				if err != nil {
					return nil, err
				}
				return result.Contents, nil
			})
		}

		var allTemplates []mgmcp.ResourceTemplate
		templatesRequest := mgmcp.ListResourceTemplatesRequest{}
		for {
			templatesResult, err := client.ListResourceTemplates(ctx, templatesRequest)
			if err != nil {
				return fmt.Errorf("failed to list resource templates from upstream: %w", err)
			}
			if len(templatesResult.ResourceTemplates) == 0 {
				break
			}
			allTemplates = append(allTemplates, templatesResult.ResourceTemplates...)
			if templatesResult.NextCursor == "" {
				break
			}
			templatesRequest.Params.Cursor = templatesResult.NextCursor
		}

		for _, template := range allTemplates {
			templateCopy := template
			templateCopy.Name = prefix + "_" + templateCopy.Name
			p.mcpServer.AddResourceTemplate(templateCopy, func(ctx context.Context, request mgmcp.ReadResourceRequest) ([]mgmcp.ResourceContents, error) {
				request.Params.URI = strings.TrimPrefix(request.Params.URI, prefix+"_")
				result, err := client.ReadResource(ctx, request)
				if err != nil {
					return nil, err
				}
				return result.Contents, nil
			})
		}
	}

	if initResult.Capabilities.Prompts != nil {
		var allPrompts []mgmcp.Prompt
		promptsRequest := mgmcp.ListPromptsRequest{}
		for {
			promptsResult, err := client.ListPrompts(ctx, promptsRequest)
			if err != nil {
				return fmt.Errorf("failed to list prompts from upstream: %w", err)
			}
			if len(promptsResult.Prompts) == 0 {
				break
			}
			allPrompts = append(allPrompts, promptsResult.Prompts...)
			if promptsResult.NextCursor == "" {
				break
			}
			promptsRequest.Params.Cursor = promptsResult.NextCursor
		}

		for _, prompt := range allPrompts {
			promptCopy := prompt
			promptCopy.Name = prefix + "_" + promptCopy.Name
			p.mcpServer.AddPrompt(promptCopy, func(ctx context.Context, request mgmcp.GetPromptRequest) (*mgmcp.GetPromptResult, error) {
				request.Params.Name = strings.TrimPrefix(request.Params.Name, prefix+"_")
				return client.GetPrompt(ctx, request)
			})
		}
	}

	return nil
}

// ServeHTTP implements the http.Handler interface
func (p *MultiProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.httpHandler == nil {
		http.Error(w, "proxy not initialized", http.StatusInternalServerError)
		return
	}
	p.httpHandler.ServeHTTP(w, r)
}
