package proxy

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"mcp-auth-proxy/pkg/auth"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
)

// SingleProxy acts as a bridge between MCP clients and servers
// It exposes an MCP server (via StreamableHTTP) and forwards
// all requests to an upstream MCP client
type SingleProxy struct {
	transport                   string
	path                        string
	client                      *Client
	mcpServer                   *mcp.Server
	httpHandler                 http.Handler
	auth                        *auth.Auth
	ctx                         context.Context
	registeredTools             []string
	registeredResources         []string
	registeredResourceTemplates []string
	registeredPrompts           []string
}

// NewSingleProxy creates a new proxy that will expose the given client
// via the specified transport (streamablehttp)
func NewSingleProxy(transport string, client *Client, path string, authMiddleware *auth.Auth) *SingleProxy {
	return &SingleProxy{
		transport: transport,
		path:      path,
		client:    client,
		auth:      authMiddleware,
	}
}

// Init initializes the proxy by creating an MCP server that forwards
// all requests to the upstream client
func (p *SingleProxy) Init(ctx context.Context) error {
	p.ctx = ctx

	// Create an MCP server that will proxy requests
	p.mcpServer = mcp.NewServer(&mcp.Implementation{
		Name:    "mcp-auth-proxy",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		Instructions: "MCP Authentication Proxy",
	})

	// Create the appropriate transport server
	switch p.transport {
	case "streamablehttp":
		p.httpHandler = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
			return p.mcpServer
		}, nil)
	default:
		return fmt.Errorf("unsupported transport: %s (must be 'streamablehttp')", p.transport)
	}

	// Register this proxy as a listener for connection events
	p.client.AddListener(p)

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
func (p *SingleProxy) OnConnected(initResult *mcp.InitializeResult) error {
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

// clearHandlers removes all tools, resources, resource templates, and prompts from the MCP server
func (p *SingleProxy) clearHandlers() {
	// Delete all previously registered handlers
	if len(p.registeredTools) > 0 {
		p.mcpServer.RemoveTools(p.registeredTools...)
		p.registeredTools = nil
	}
	if len(p.registeredResources) > 0 {
		p.mcpServer.RemoveResources(p.registeredResources...)
		p.registeredResources = nil
	}
	if len(p.registeredResourceTemplates) > 0 {
		p.mcpServer.RemoveResourceTemplates(p.registeredResourceTemplates...)
		p.registeredResourceTemplates = nil
	}
	if len(p.registeredPrompts) > 0 {
		p.mcpServer.RemovePrompts(p.registeredPrompts...)
		p.registeredPrompts = nil
	}
}

// setupProxyHandlers configures the MCP server to forward all requests to the upstream client
func (p *SingleProxy) setupProxyHandlers(ctx context.Context, initResult *mcp.InitializeResult) error {
	// Configure server capabilities to match upstream capabilities
	if initResult.Capabilities.Tools != nil {
		// Fetch all tools from upstream
		var allTools []*mcp.Tool
		nextCursor := ""
		for {
			toolsResult, err := p.client.ListTools(ctx, &mcp.ListToolsParams{Cursor: nextCursor})
			if err != nil {
				return fmt.Errorf("failed to list tools from upstream: %w", err)
			}
			if len(toolsResult.Tools) == 0 {
				break
			}
			allTools = append(allTools, toolsResult.Tools...)
			nextCursor = toolsResult.NextCursor
			if nextCursor == "" {
				break
			}
		}

		// Register each tool with a proxy handler
		for _, tool := range allTools {
			toolCopy := tool // Capture for closure
			p.mcpServer.AddTool(toolCopy, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				// Forward the tool call to the upstream client
				return p.client.CallTool(ctx, &mcp.CallToolParams{
					Name:      req.Params.Name,
					Arguments: req.Params.Arguments,
				})
			})
			p.registeredTools = append(p.registeredTools, tool.Name)
		}
	}

	if initResult.Capabilities.Resources != nil {
		// Fetch all resources from upstream
		var allResources []*mcp.Resource
		nextCursor := ""
		for {
			resourcesResult, err := p.client.ListResources(ctx, &mcp.ListResourcesParams{Cursor: nextCursor})
			if err != nil {
				return fmt.Errorf("failed to list resources from upstream: %w", err)
			}
			if len(resourcesResult.Resources) == 0 {
				break
			}
			allResources = append(allResources, resourcesResult.Resources...)
			nextCursor = resourcesResult.NextCursor
			if nextCursor == "" {
				break
			}
		}

		// Register each resource with a proxy handler
		for _, resource := range allResources {
			resourceCopy := resource // Capture for closure
			p.mcpServer.AddResource(resourceCopy, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				// Forward the resource read to the upstream client
				return p.client.ReadResource(ctx, &mcp.ReadResourceParams{URI: req.Params.URI})
			})
			p.registeredResources = append(p.registeredResources, resource.URI)
		}

		// Fetch all resource templates from upstream
		var allResourceTemplates []*mcp.ResourceTemplate
		nextCursor = ""
		for {
			templatesResult, err := p.client.ListResourceTemplates(ctx, &mcp.ListResourceTemplatesParams{Cursor: nextCursor})
			if err != nil {
				return fmt.Errorf("failed to list resource templates from upstream: %w", err)
			}
			if len(templatesResult.ResourceTemplates) == 0 {
				break
			}
			allResourceTemplates = append(allResourceTemplates, templatesResult.ResourceTemplates...)
			nextCursor = templatesResult.NextCursor
			if nextCursor == "" {
				break
			}
		}

		// Register each resource template with a proxy handler
		for _, template := range allResourceTemplates {
			templateCopy := template // Capture for closure
			p.mcpServer.AddResourceTemplate(templateCopy, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				// Forward the resource read to the upstream client
				return p.client.ReadResource(ctx, &mcp.ReadResourceParams{URI: req.Params.URI})
			})
			p.registeredResourceTemplates = append(p.registeredResourceTemplates, template.URITemplate)
		}
	}

	if initResult.Capabilities.Prompts != nil {
		// Fetch all prompts from upstream
		var allPrompts []*mcp.Prompt
		nextCursor := ""
		for {
			promptsResult, err := p.client.ListPrompts(ctx, &mcp.ListPromptsParams{Cursor: nextCursor})
			if err != nil {
				return fmt.Errorf("failed to list prompts from upstream: %w", err)
			}
			if len(promptsResult.Prompts) == 0 {
				break
			}
			allPrompts = append(allPrompts, promptsResult.Prompts...)
			nextCursor = promptsResult.NextCursor
			if nextCursor == "" {
				break
			}
		}

		// Register each prompt with a proxy handler
		for _, prompt := range allPrompts {
			promptCopy := prompt // Capture for closure
			p.mcpServer.AddPrompt(promptCopy, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				// Forward the prompt request to the upstream client
				return p.client.GetPrompt(ctx, &mcp.GetPromptParams{
					Name:      req.Params.Name,
					Arguments: req.Params.Arguments,
				})
			})
			p.registeredPrompts = append(p.registeredPrompts, prompt.Name)
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
	p.auth.Middleware(p.httpHandler).ServeHTTP(w, r)
}

// multiProxyClientListener is a helper that implements ConnectionListener for MultiProxy
type multiProxyClientListener struct {
	proxy      *MultiProxy
	clientName string
	prefix     string
}

func (l *multiProxyClientListener) OnConnected(initResult *mcp.InitializeResult) error {
	return l.proxy.onClientConnected(l.clientName, l.prefix, initResult)
}

// MultiProxy acts as a bridge between multiple MCP clients and a single server endpoint
type MultiProxy struct {
	transport           string
	path                string
	clients             map[string]*Client
	mcps                []MultiMCPConfig
	mcpServer           *mcp.Server
	httpHandler         http.Handler
	auth                *auth.Auth
	ctx                 context.Context
	mu                  sync.RWMutex        // protects registeredTools, registeredRes, registeredResTemplates, registeredPropts
	registeredTools     map[string][]string // client name -> tool names
	registeredRes       map[string][]string // client name -> resource URIs
	registeredResTemplates map[string][]string // client name -> resource template URIs
	registeredPropts    map[string][]string // client name -> prompt names
}

// NewMultiProxy creates a new multi-proxy
func NewMultiProxy(
	transport string,
	clients map[string]*Client,
	mcps []MultiMCPConfig,
	path string,
	authMiddleware *auth.Auth,
) *MultiProxy {
	return &MultiProxy{
		transport:              transport,
		path:                   path,
		clients:                clients,
		mcps:                   mcps,
		auth:                   authMiddleware,
		registeredTools:        make(map[string][]string),
		registeredRes:          make(map[string][]string),
		registeredResTemplates: make(map[string][]string),
		registeredPropts:       make(map[string][]string),
	}
}

// Init initializes the multi-proxy
func (p *MultiProxy) Init(ctx context.Context) error {
	p.ctx = ctx

	p.mcpServer = mcp.NewServer(&mcp.Implementation{
		Name:    "mcp-auth-multi-proxy",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		Instructions: "MCP Authentication Multi-Proxy",
	})

	// Create the appropriate transport server
	switch p.transport {
	case "streamablehttp":
		p.httpHandler = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
			return p.mcpServer
		}, nil)
	default:
		return fmt.Errorf("unsupported transport: %s (must be 'streamablehttp')", p.transport)
	}

	// Register this proxy as a listener for each client and set up initial handlers
	for _, mcpConfig := range p.mcps {
		client, ok := p.clients[mcpConfig.Name]
		if !ok {
			return fmt.Errorf("client not found for mcp config: %s", mcpConfig.Name)
		}

		// Register as listener
		client.AddListener(&multiProxyClientListener{
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
func (p *MultiProxy) onClientConnected(clientName, prefix string, initResult *mcp.InitializeResult) error {
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
	p.mu.Lock()
	defer p.mu.Unlock()

	// Delete tools for this client
	if toolNames, ok := p.registeredTools[clientName]; ok && len(toolNames) > 0 {
		p.mcpServer.RemoveTools(toolNames...)
		delete(p.registeredTools, clientName)
	}

	// Delete resources for this client
	if resURIs, ok := p.registeredRes[clientName]; ok && len(resURIs) > 0 {
		p.mcpServer.RemoveResources(resURIs...)
		delete(p.registeredRes, clientName)
	}

	// Delete resource templates for this client
	if resTemplateURIs, ok := p.registeredResTemplates[clientName]; ok && len(resTemplateURIs) > 0 {
		p.mcpServer.RemoveResourceTemplates(resTemplateURIs...)
		delete(p.registeredResTemplates, clientName)
	}

	// Delete prompts for this client
	if promptNames, ok := p.registeredPropts[clientName]; ok && len(promptNames) > 0 {
		p.mcpServer.RemovePrompts(promptNames...)
		delete(p.registeredPropts, clientName)
	}
}

// setupProxyHandlers configures the MCP server to forward all requests to the upstream client
func (p *MultiProxy) setupProxyHandlers(ctx context.Context, client *Client, initResult *mcp.InitializeResult, clientName, prefix string) error {
	if initResult.Capabilities.Tools != nil {
		var allTools []*mcp.Tool
		nextCursor := ""
		for {
			toolsResult, err := client.ListTools(ctx, &mcp.ListToolsParams{Cursor: nextCursor})
			if err != nil {
				return fmt.Errorf("failed to list tools from upstream: %w", err)
			}
			if len(toolsResult.Tools) == 0 {
				break
			}
			allTools = append(allTools, toolsResult.Tools...)
			nextCursor = toolsResult.NextCursor
			if nextCursor == "" {
				break
			}
		}

		var toolNames []string
		for _, tool := range allTools {
			toolCopy := *tool // Dereference and copy
			toolCopy.Name = prefix + "_" + toolCopy.Name
			p.mcpServer.AddTool(&toolCopy, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				originalName := strings.TrimPrefix(req.Params.Name, prefix+"_")
				return client.CallTool(ctx, &mcp.CallToolParams{
					Name:      originalName,
					Arguments: req.Params.Arguments,
				})
			})
			toolNames = append(toolNames, toolCopy.Name)
		}

		p.mu.Lock()
		p.registeredTools[clientName] = append(p.registeredTools[clientName], toolNames...)
		p.mu.Unlock()
	}

	if initResult.Capabilities.Resources != nil {
		var allResources []*mcp.Resource
		nextCursor := ""
		for {
			resourcesResult, err := client.ListResources(ctx, &mcp.ListResourcesParams{Cursor: nextCursor})
			if err != nil {
				return fmt.Errorf("failed to list resources from upstream: %w", err)
			}
			if len(resourcesResult.Resources) == 0 {
				break
			}
			allResources = append(allResources, resourcesResult.Resources...)
			nextCursor = resourcesResult.NextCursor
			if nextCursor == "" {
				break
			}
		}

		var resourceURIs []string
		for _, resource := range allResources {
			resourceCopy := *resource // Dereference and copy
			resourceCopy.Name = prefix + "_" + resourceCopy.Name
			p.mcpServer.AddResource(&resourceCopy, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				originalURI := strings.TrimPrefix(req.Params.URI, prefix+"_")
				return client.ReadResource(ctx, &mcp.ReadResourceParams{URI: originalURI})
			})
			resourceURIs = append(resourceURIs, resourceCopy.URI)
		}

		p.mu.Lock()
		p.registeredRes[clientName] = append(p.registeredRes[clientName], resourceURIs...)
		p.mu.Unlock()

		// Fetch all resource templates from upstream
		var allResourceTemplates []*mcp.ResourceTemplate
		nextCursor = ""
		for {
			templatesResult, err := client.ListResourceTemplates(ctx, &mcp.ListResourceTemplatesParams{Cursor: nextCursor})
			if err != nil {
				return fmt.Errorf("failed to list resource templates from upstream: %w", err)
			}
			if len(templatesResult.ResourceTemplates) == 0 {
				break
			}
			allResourceTemplates = append(allResourceTemplates, templatesResult.ResourceTemplates...)
			nextCursor = templatesResult.NextCursor
			if nextCursor == "" {
				break
			}
		}

		// Register each resource template with a proxy handler
		var templateURIs []string
		for _, template := range allResourceTemplates {
			templateCopy := *template // Dereference and copy
			templateCopy.Name = prefix + "_" + templateCopy.Name
			p.mcpServer.AddResourceTemplate(&templateCopy, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				originalURI := strings.TrimPrefix(req.Params.URI, prefix+"_")
				return client.ReadResource(ctx, &mcp.ReadResourceParams{URI: originalURI})
			})
			templateURIs = append(templateURIs, templateCopy.URITemplate)
		}

		p.mu.Lock()
		p.registeredResTemplates[clientName] = append(p.registeredResTemplates[clientName], templateURIs...)
		p.mu.Unlock()
	}

	if initResult.Capabilities.Prompts != nil {
		var allPrompts []*mcp.Prompt
		nextCursor := ""
		for {
			promptsResult, err := client.ListPrompts(ctx, &mcp.ListPromptsParams{Cursor: nextCursor})
			if err != nil {
				return fmt.Errorf("failed to list prompts from upstream: %w", err)
			}
			if len(promptsResult.Prompts) == 0 {
				break
			}
			allPrompts = append(allPrompts, promptsResult.Prompts...)
			nextCursor = promptsResult.NextCursor
			if nextCursor == "" {
				break
			}
		}

		var promptNames []string
		for _, prompt := range allPrompts {
			promptCopy := *prompt // Dereference and copy
			promptCopy.Name = prefix + "_" + promptCopy.Name
			p.mcpServer.AddPrompt(&promptCopy, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				originalName := strings.TrimPrefix(req.Params.Name, prefix+"_")
				return client.GetPrompt(ctx, &mcp.GetPromptParams{
					Name:      originalName,
					Arguments: req.Params.Arguments,
				})
			})
			promptNames = append(promptNames, promptCopy.Name)
		}

		p.mu.Lock()
		p.registeredPropts[clientName] = append(p.registeredPropts[clientName], promptNames...)
		p.mu.Unlock()
	}

	return nil
}

// ServeHTTP implements the http.Handler interface
func (p *MultiProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.httpHandler == nil {
		http.Error(w, "proxy not initialized", http.StatusInternalServerError)
		return
	}
	p.auth.Middleware(p.httpHandler).ServeHTTP(w, r)
}
