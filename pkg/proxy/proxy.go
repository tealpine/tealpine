package proxy

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	mgmcp "github.com/mark3labs/mcp-go/mcp"
	mgserver "github.com/mark3labs/mcp-go/server"
	"github.com/sirupsen/logrus"
)

// SingleProxy acts as a bridge between MCP clients and servers
// It exposes an MCP server (via StreamableHTTP or SSE) and forwards
// all requests to an upstream MCP client
type SingleProxy struct {
	transport        string
	path             string
	client           *Client
	mcpServer        *mgserver.MCPServer
	httpHandler      http.Handler
	ctx              context.Context
	registeredTools  []string
	registeredRes    []string
	registeredPropts []string
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
	p.ctx = ctx

	// Create an MCP server that will proxy requests
	p.mcpServer = mgserver.NewMCPServer(
		"mcp-auth-proxy",
		"1.0.0",
		mgserver.WithInstructions("MCP Authentication Proxy"),
	)

	// Create the appropriate transport server
	switch p.transport {
	case "streamablehttp":
		p.httpHandler = mgserver.NewStreamableHTTPServer(p.mcpServer)
	case "sse":
		p.httpHandler = mgserver.NewSSEServer(p.mcpServer, mgserver.WithStaticBasePath(p.path))
	default:
		return fmt.Errorf("unsupported transport: %s (must be 'streamablehttp' or 'sse')", p.transport)
	}

	// Register this proxy as a listener for connection events
	p.client.RegisterListener(p)

	// If client is already connected, set up handlers immediately
	if initResult := p.client.GetInitResult(); initResult != nil {
		if err := p.setupProxyHandlers(ctx, initResult); err != nil {
			return fmt.Errorf("failed to setup proxy handlers: %w", err)
		}
	}

	return nil
}

// OnConnected implements ConnectionListener interface
// Called when the client connects or reconnects to the upstream MCP server
func (p *SingleProxy) OnConnected(initResult *mgmcp.InitializeResult) error {
	logrus.Info("SingleProxy.OnConnected called - updating handlers")
	// Clear all existing handlers before registering new ones
	p.clearHandlers()

	// Set up handlers with the new capabilities
	if err := p.setupProxyHandlers(p.ctx, initResult); err != nil {
		logrus.WithError(err).Error("Failed to setup proxy handlers on reconnect")
		return err
	}
	logrus.Info("SingleProxy.OnConnected completed successfully")
	return nil
}

// clearHandlers removes all tools, resources, and prompts from the MCP server
func (p *SingleProxy) clearHandlers() {
	// Delete all previously registered handlers
	if len(p.registeredTools) > 0 {
		p.mcpServer.DeleteTools(p.registeredTools...)
		p.registeredTools = nil
	}
	if len(p.registeredRes) > 0 {
		p.mcpServer.DeleteResources(p.registeredRes...)
		p.registeredRes = nil
	}
	if len(p.registeredPropts) > 0 {
		p.mcpServer.DeletePrompts(p.registeredPropts...)
		p.registeredPropts = nil
	}
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
			p.registeredTools = append(p.registeredTools, tool.Name)
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
			p.registeredRes = append(p.registeredRes, resource.URI)
		}

		// Note: Resource templates are not registered because they cannot be deleted,
		// which would cause issues on reconnection. Clients should use regular resources instead.
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
			p.registeredPropts = append(p.registeredPropts, prompt.Name)
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

// multiProxyClientListener is a helper that implements ConnectionListener for MultiProxy
type multiProxyClientListener struct {
	proxy      *MultiProxy
	clientName string
	prefix     string
}

func (l *multiProxyClientListener) OnConnected(initResult *mgmcp.InitializeResult) error {
	return l.proxy.onClientConnected(l.clientName, l.prefix, initResult)
}

// MultiProxy acts as a bridge between multiple MCP clients and a single server endpoint
type MultiProxy struct {
	transport        string
	path             string
	clients          map[string]*Client
	mcps             []MultiMCPConfig
	mcpServer        *mgserver.MCPServer
	httpHandler      http.Handler
	ctx              context.Context
	registeredTools  map[string][]string // client name -> tool names
	registeredRes    map[string][]string // client name -> resource URIs
	registeredPropts map[string][]string // client name -> prompt names
}

// NewMultiProxy creates a new multi-proxy
func NewMultiProxy(
	transport string,
	clients map[string]*Client,
	mcps []MultiMCPConfig,
	path string,
) *MultiProxy {
	return &MultiProxy{
		transport:        transport,
		path:             path,
		clients:          clients,
		mcps:             mcps,
		registeredTools:  make(map[string][]string),
		registeredRes:    make(map[string][]string),
		registeredPropts: make(map[string][]string),
	}
}

// Init initializes the multi-proxy
func (p *MultiProxy) Init(ctx context.Context) error {
	p.ctx = ctx

	p.mcpServer = mgserver.NewMCPServer(
		"mcp-auth-multi-proxy",
		"1.0.0",
		mgserver.WithInstructions("MCP Authentication Multi-Proxy"),
	)

	// Create the appropriate transport server
	switch p.transport {
	case "streamablehttp":
		p.httpHandler = mgserver.NewStreamableHTTPServer(p.mcpServer)
	case "sse":
		p.httpHandler = mgserver.NewSSEServer(p.mcpServer, mgserver.WithStaticBasePath(p.path))
	default:
		return fmt.Errorf("unsupported transport: %s (must be 'streamablehttp' or 'sse')", p.transport)
	}

	// Register this proxy as a listener for each client and set up initial handlers
	for _, mcpConfig := range p.mcps {
		client, ok := p.clients[mcpConfig.Name]
		if !ok {
			return fmt.Errorf("client not found for mcp config: %s", mcpConfig.Name)
		}

		// Register as listener
		client.RegisterListener(&multiProxyClientListener{
			proxy:      p,
			clientName: mcpConfig.Name,
			prefix:     mcpConfig.Prefix,
		})

		// If client is already connected, set up handlers immediately
		if initResult := client.GetInitResult(); initResult != nil {
			if err := p.setupProxyHandlers(ctx, client, initResult, mcpConfig.Name, mcpConfig.Prefix); err != nil {
				return fmt.Errorf("failed to setup proxy handlers for client %s: %w", mcpConfig.Name, err)
			}
		}
	}

	return nil
}

// onClientConnected is called when a client connects or reconnects
func (p *MultiProxy) onClientConnected(clientName, prefix string, initResult *mgmcp.InitializeResult) error {
	// Clear handlers for this specific client
	p.clearClientHandlers(clientName)

	// Get the client
	client, ok := p.clients[clientName]
	if !ok {
		return fmt.Errorf("client not found: %s", clientName)
	}

	// Set up handlers with the new capabilities
	return p.setupProxyHandlers(p.ctx, client, initResult, clientName, prefix)
}

// clearClientHandlers removes all handlers for a specific client
func (p *MultiProxy) clearClientHandlers(clientName string) {
	// Delete tools for this client
	if toolNames, ok := p.registeredTools[clientName]; ok && len(toolNames) > 0 {
		p.mcpServer.DeleteTools(toolNames...)
		delete(p.registeredTools, clientName)
	}

	// Delete resources for this client
	if resURIs, ok := p.registeredRes[clientName]; ok && len(resURIs) > 0 {
		p.mcpServer.DeleteResources(resURIs...)
		delete(p.registeredRes, clientName)
	}

	// Delete prompts for this client
	if promptNames, ok := p.registeredPropts[clientName]; ok && len(promptNames) > 0 {
		p.mcpServer.DeletePrompts(promptNames...)
		delete(p.registeredPropts, clientName)
	}
}

// setupProxyHandlers configures the MCP server to forward all requests to the upstream client
func (p *MultiProxy) setupProxyHandlers(ctx context.Context, client *Client, initResult *mgmcp.InitializeResult, clientName, prefix string) error {
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
			p.registeredTools[clientName] = append(p.registeredTools[clientName], toolCopy.Name)
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
			p.registeredRes[clientName] = append(p.registeredRes[clientName], resourceCopy.URI)
		}

		// Note: Resource templates are not registered because they cannot be deleted,
		// which would cause issues on reconnection. Clients should use regular resources instead.
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
			p.registeredPropts[clientName] = append(p.registeredPropts[clientName], promptCopy.Name)
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
