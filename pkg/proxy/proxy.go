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

// HandlerNamingStrategy defines how to transform names and URIs for handlers
type HandlerNamingStrategy interface {
	TransformName(name string) string
	TransformURI(uri string) string
}

// HandlerRegistry defines how to store and clear registered handlers
type HandlerRegistry interface {
	AddToolName(name string)
	AddResourceURI(uri string)
	AddResourceTemplateURI(uri string)
	AddPromptName(name string)
	ClearAll()
	GetToolNames() []string
	GetResourceURIs() []string
	GetResourceTemplateURIs() []string
	GetPromptNames() []string
}

// proxyCore contains shared functionality for both SingleProxy and MultiProxy
type proxyCore struct {
	mcpServer     *mcp.Server
	httpHandler   http.Handler
	authenticator *auth.Authenticator
	authorizer    *auth.Authorizer
}

// newProxyCore creates a new proxy core with common initialization
func newProxyCore(name, version string, authenticator *auth.Authenticator, authorizer *auth.Authorizer, transport string) (*proxyCore, error) {
	core := &proxyCore{
		authenticator: authenticator,
		authorizer:    authorizer,
	}

	// Create MCP server
	core.mcpServer = mcp.NewServer(&mcp.Implementation{
		Name:    name,
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: "MCP Authentication Proxy",
	})

	// Add authorization middleware
	core.mcpServer.AddReceivingMiddleware(authorizer.Middleware)

	// Create HTTP handler
	switch transport {
	case "streamablehttp":
		core.httpHandler = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
			return core.mcpServer
		}, nil)
	default:
		return nil, fmt.Errorf("unsupported transport: %s (must be 'streamablehttp')", transport)
	}

	return core, nil
}

// ServeHTTP implements the http.Handler interface
func (p *proxyCore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.httpHandler == nil {
		http.Error(w, "proxy not initialized", http.StatusInternalServerError)
		return
	}
	p.authenticator.Middleware(p.httpHandler).ServeHTTP(w, r)
}

// fetchAllTools fetches all tools from upstream using pagination
func fetchAllTools(ctx context.Context, client *Client) ([]*mcp.Tool, error) {
	var allTools []*mcp.Tool
	nextCursor := ""
	for {
		toolsResult, err := client.ListTools(ctx, &mcp.ListToolsParams{Cursor: nextCursor})
		if err != nil {
			return nil, fmt.Errorf("failed to list tools from upstream: %w", err)
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
	return allTools, nil
}

// fetchAllResources fetches all resources from upstream using pagination
func fetchAllResources(ctx context.Context, client *Client) ([]*mcp.Resource, error) {
	var allResources []*mcp.Resource
	nextCursor := ""
	for {
		resourcesResult, err := client.ListResources(ctx, &mcp.ListResourcesParams{Cursor: nextCursor})
		if err != nil {
			return nil, fmt.Errorf("failed to list resources from upstream: %w", err)
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
	return allResources, nil
}

// fetchAllResourceTemplates fetches all resource templates from upstream using pagination
func fetchAllResourceTemplates(ctx context.Context, client *Client) ([]*mcp.ResourceTemplate, error) {
	var allTemplates []*mcp.ResourceTemplate
	nextCursor := ""
	for {
		templatesResult, err := client.ListResourceTemplates(ctx, &mcp.ListResourceTemplatesParams{Cursor: nextCursor})
		if err != nil {
			return nil, fmt.Errorf("failed to list resource templates from upstream: %w", err)
		}
		if len(templatesResult.ResourceTemplates) == 0 {
			break
		}
		allTemplates = append(allTemplates, templatesResult.ResourceTemplates...)
		nextCursor = templatesResult.NextCursor
		if nextCursor == "" {
			break
		}
	}
	return allTemplates, nil
}

// fetchAllPrompts fetches all prompts from upstream using pagination
func fetchAllPrompts(ctx context.Context, client *Client) ([]*mcp.Prompt, error) {
	var allPrompts []*mcp.Prompt
	nextCursor := ""
	for {
		promptsResult, err := client.ListPrompts(ctx, &mcp.ListPromptsParams{Cursor: nextCursor})
		if err != nil {
			return nil, fmt.Errorf("failed to list prompts from upstream: %w", err)
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
	return allPrompts, nil
}

// setupProxyHandlersWithStrategy sets up proxy handlers using the provided strategies
func setupProxyHandlersWithStrategy(
	ctx context.Context,
	mcpServer *mcp.Server,
	client *Client,
	initResult *mcp.InitializeResult,
	naming HandlerNamingStrategy,
	registry HandlerRegistry,
) error {
	// Setup tools
	if initResult.Capabilities.Tools != nil {
		tools, err := fetchAllTools(ctx, client)
		if err != nil {
			return err
		}

		for _, tool := range tools {
			toolCopy := *tool
			originalName := tool.Name // Capture original name before transformation
			toolCopy.Name = naming.TransformName(tool.Name)
			mcpServer.AddTool(&toolCopy, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				// Use captured original name
				return client.CallTool(ctx, &mcp.CallToolParams{
					Name:      originalName,
					Arguments: req.Params.Arguments,
				})
			})
			registry.AddToolName(toolCopy.Name)
		}
	}

	// Setup resources
	if initResult.Capabilities.Resources != nil {
		resources, err := fetchAllResources(ctx, client)
		if err != nil {
			return err
		}

		for _, resource := range resources {
			resourceCopy := *resource
			originalURI := resource.URI // Capture original URI before transformation
			resourceCopy.Name = naming.TransformName(resource.Name)
			resourceCopy.URI = naming.TransformURI(resource.URI)
			mcpServer.AddResource(&resourceCopy, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				// Use captured original URI
				return client.ReadResource(ctx, &mcp.ReadResourceParams{URI: originalURI})
			})
			registry.AddResourceURI(resourceCopy.URI)
		}

		// Setup resource templates
		templates, err := fetchAllResourceTemplates(ctx, client)
		if err != nil {
			return err
		}

		for _, template := range templates {
			templateCopy := *template
			templateCopy.Name = naming.TransformName(template.Name)
			templateCopy.URITemplate = naming.TransformURI(template.URITemplate)
			mcpServer.AddResourceTemplate(&templateCopy, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				// For templates, we need to map the transformed URI back to the original
				// The request URI will have the transformed prefix, we need to pass the original
				transformedPrefix := naming.TransformURI("")
				originalRequestURI := strings.TrimPrefix(req.Params.URI, transformedPrefix)
				return client.ReadResource(ctx, &mcp.ReadResourceParams{URI: originalRequestURI})
			})
			registry.AddResourceTemplateURI(templateCopy.URITemplate)
		}
	}

	// Setup prompts
	if initResult.Capabilities.Prompts != nil {
		prompts, err := fetchAllPrompts(ctx, client)
		if err != nil {
			return err
		}

		for _, prompt := range prompts {
			promptCopy := *prompt
			originalName := prompt.Name // Capture original name before transformation
			promptCopy.Name = naming.TransformName(prompt.Name)
			mcpServer.AddPrompt(&promptCopy, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				// Use captured original name
				return client.GetPrompt(ctx, &mcp.GetPromptParams{
					Name:      originalName,
					Arguments: req.Params.Arguments,
				})
			})
			registry.AddPromptName(promptCopy.Name)
		}
	}

	return nil
}

// watchClientEvents is a generic event watcher that monitors a client for events
// and calls the refresh handler for relevant event types
func watchClientEvents(ctx context.Context, client *Client, refreshHandler func(EventType) error) {
	for {
		// Wait for the next event
		event, err := client.WaitForEvent(ctx)
		if err != nil {
			// Context canceled or closed
			return
		}

		switch event.Type {
		case EventConnected, EventToolsListChanged, EventResourcesListChanged, EventPromptsListChanged, EventResourceTemplatesListChanged:
			if err := refreshHandler(event.Type); err != nil {
				logrus.WithError(err).Errorf("Failed to refresh handlers after %s", event.Type.String())
				continue
			}
		default:
			continue
		}

		// Check if another event occurred during processing
		// If so, handle it immediately instead of blocking on WaitForEvent
		if lastEvent := client.GetLastEvent(); lastEvent != nil && !lastEvent.Timestamp.Equal(event.Timestamp) {
			logrus.Debug("New event detected during processing, handling immediately")
			continue
		}
	}
}

// singleProxyNamingStrategy implements HandlerNamingStrategy for SingleProxy (no transformation)
type singleProxyNamingStrategy struct{}

func (s *singleProxyNamingStrategy) TransformName(name string) string { return name }
func (s *singleProxyNamingStrategy) TransformURI(uri string) string   { return uri }

// SingleProxy acts as a bridge between MCP clients and servers
// It exposes an MCP server (via StreamableHTTP) and forwards
// all requests to an upstream MCP client
type SingleProxy struct {
	*proxyCore
	transport         string
	path              string
	client            *Client
	ctx               context.Context
	naming            HandlerNamingStrategy
	tools             []string
	resources         []string
	resourceTemplates []string
	prompts           []string
}

// NewSingleProxy creates a new proxy that will expose the given client
// via the specified transport (streamablehttp)
func NewSingleProxy(transport string, client *Client, path string, authenticator *auth.Authenticator, authorizer *auth.Authorizer) (*SingleProxy, error) {
	// Create proxy core with shared initialization
	core, err := newProxyCore("mcp-auth-proxy", "1.0.0", authenticator, authorizer, transport)
	if err != nil {
		return nil, err
	}

	return &SingleProxy{
		proxyCore: core,
		transport: transport,
		path:      path,
		client:    client,
		naming:    &singleProxyNamingStrategy{},
	}, nil
}

// HandlerRegistry implementation for SingleProxy
func (p *SingleProxy) AddToolName(name string)           { p.tools = append(p.tools, name) }
func (p *SingleProxy) AddResourceURI(uri string)         { p.resources = append(p.resources, uri) }
func (p *SingleProxy) AddResourceTemplateURI(uri string) { p.resourceTemplates = append(p.resourceTemplates, uri) }
func (p *SingleProxy) AddPromptName(name string)         { p.prompts = append(p.prompts, name) }
func (p *SingleProxy) GetToolNames() []string            { return p.tools }
func (p *SingleProxy) GetResourceURIs() []string         { return p.resources }
func (p *SingleProxy) GetResourceTemplateURIs() []string { return p.resourceTemplates }
func (p *SingleProxy) GetPromptNames() []string          { return p.prompts }

func (p *SingleProxy) ClearAll() {
	if len(p.tools) > 0 {
		p.mcpServer.RemoveTools(p.tools...)
		p.tools = nil
	}
	if len(p.resources) > 0 {
		p.mcpServer.RemoveResources(p.resources...)
		p.resources = nil
	}
	if len(p.resourceTemplates) > 0 {
		p.mcpServer.RemoveResourceTemplates(p.resourceTemplates...)
		p.resourceTemplates = nil
	}
	if len(p.prompts) > 0 {
		p.mcpServer.RemovePrompts(p.prompts...)
		p.prompts = nil
	}
}

// Init initializes the proxy by creating an MCP server that forwards
// all requests to the upstream client
func (p *SingleProxy) Init(ctx context.Context) error {
	p.ctx = ctx

	// If client is already connected, set up handlers immediately
	if initResult := p.client.GetInitResult(); initResult != nil {
		if err := p.setupProxyHandlers(ctx, initResult); err != nil {
			return fmt.Errorf("failed to setup proxy handlers: %w", err)
		}
	}

	// Start a goroutine to watch for connection/reconnection events
	go p.watchConnectionEvents(ctx)

	return nil
}

// watchConnectionEvents monitors the client for connection/reconnection events
// and updates the proxy handlers accordingly
func (p *SingleProxy) watchConnectionEvents(ctx context.Context) {
	watchClientEvents(ctx, p.client, func(eventType EventType) error {
		return p.refreshHandlers(p.ctx, eventType)
	})
}

// clearHandlers removes all tools, resources, resource templates, and prompts from the MCP server
func (p *SingleProxy) clearHandlers() {
	p.ClearAll()
}

// setupProxyHandlers configures the MCP server to forward all requests to the upstream client
func (p *SingleProxy) setupProxyHandlers(ctx context.Context, initResult *mcp.InitializeResult) error {
	return setupProxyHandlersWithStrategy(ctx, p.mcpServer, p.client, initResult, p.naming, p)
}

// refreshHandlers clears and re-registers all handlers for this proxy
func (p *SingleProxy) refreshHandlers(ctx context.Context, eventType EventType) error {
	logrus.Infof("SingleProxy: %s - refreshing handlers", eventType.String())

	// Clear all existing handlers
	p.clearHandlers()

	// Get the current initialization result
	initResult := p.client.GetInitResult()
	if initResult == nil {
		return fmt.Errorf("initResult is nil")
	}

	// Set up handlers with the updated capabilities
	if err := p.setupProxyHandlers(ctx, initResult); err != nil {
		return fmt.Errorf("failed to setup handlers: %w", err)
	}

	logrus.Infof("SingleProxy: handlers refreshed successfully after %s", eventType.String())
	return nil
}

// ==================== MultiProxy Strategy Implementations ====================

// multiProxyNamingStrategy applies prefix transformation to all names
type multiProxyNamingStrategy struct {
	prefix string
}

func (m *multiProxyNamingStrategy) TransformName(name string) string {
	return m.prefix + "_" + name
}

func (m *multiProxyNamingStrategy) TransformURI(uri string) string {
	return m.prefix + "_" + uri
}

// multiProxyClientRegistry is a lightweight wrapper that implements HandlerRegistry
// for a specific client in MultiProxy
type multiProxyClientRegistry struct {
	proxy      *MultiProxy
	clientName string
}

func (m *multiProxyClientRegistry) AddToolName(name string) {
	m.proxy.mu.Lock()
	defer m.proxy.mu.Unlock()
	m.proxy.registeredTools[m.clientName] = append(m.proxy.registeredTools[m.clientName], name)
}

func (m *multiProxyClientRegistry) AddResourceURI(uri string) {
	m.proxy.mu.Lock()
	defer m.proxy.mu.Unlock()
	m.proxy.registeredRes[m.clientName] = append(m.proxy.registeredRes[m.clientName], uri)
}

func (m *multiProxyClientRegistry) AddResourceTemplateURI(uri string) {
	m.proxy.mu.Lock()
	defer m.proxy.mu.Unlock()
	m.proxy.registeredResTemplates[m.clientName] = append(m.proxy.registeredResTemplates[m.clientName], uri)
}

func (m *multiProxyClientRegistry) AddPromptName(name string) {
	m.proxy.mu.Lock()
	defer m.proxy.mu.Unlock()
	m.proxy.registeredPropts[m.clientName] = append(m.proxy.registeredPropts[m.clientName], name)
}

func (m *multiProxyClientRegistry) ClearAll() {
	m.proxy.mu.Lock()
	defer m.proxy.mu.Unlock()

	// Delete tools for this client
	if toolNames, ok := m.proxy.registeredTools[m.clientName]; ok && len(toolNames) > 0 {
		m.proxy.mcpServer.RemoveTools(toolNames...)
		delete(m.proxy.registeredTools, m.clientName)
	}

	// Delete resources for this client
	if resURIs, ok := m.proxy.registeredRes[m.clientName]; ok && len(resURIs) > 0 {
		m.proxy.mcpServer.RemoveResources(resURIs...)
		delete(m.proxy.registeredRes, m.clientName)
	}

	// Delete resource templates for this client
	if resTemplateURIs, ok := m.proxy.registeredResTemplates[m.clientName]; ok && len(resTemplateURIs) > 0 {
		m.proxy.mcpServer.RemoveResourceTemplates(resTemplateURIs...)
		delete(m.proxy.registeredResTemplates, m.clientName)
	}

	// Delete prompts for this client
	if promptNames, ok := m.proxy.registeredPropts[m.clientName]; ok && len(promptNames) > 0 {
		m.proxy.mcpServer.RemovePrompts(promptNames...)
		delete(m.proxy.registeredPropts, m.clientName)
	}
}

func (m *multiProxyClientRegistry) GetToolNames() []string {
	m.proxy.mu.RLock()
	defer m.proxy.mu.RUnlock()
	if names, ok := m.proxy.registeredTools[m.clientName]; ok {
		return names
	}
	return nil
}

func (m *multiProxyClientRegistry) GetResourceURIs() []string {
	m.proxy.mu.RLock()
	defer m.proxy.mu.RUnlock()
	if uris, ok := m.proxy.registeredRes[m.clientName]; ok {
		return uris
	}
	return nil
}

func (m *multiProxyClientRegistry) GetResourceTemplateURIs() []string {
	m.proxy.mu.RLock()
	defer m.proxy.mu.RUnlock()
	if uris, ok := m.proxy.registeredResTemplates[m.clientName]; ok {
		return uris
	}
	return nil
}

func (m *multiProxyClientRegistry) GetPromptNames() []string {
	m.proxy.mu.RLock()
	defer m.proxy.mu.RUnlock()
	if names, ok := m.proxy.registeredPropts[m.clientName]; ok {
		return names
	}
	return nil
}

// ==================== MultiProxy ====================

// MultiProxy acts as a bridge between multiple MCP clients and a single server endpoint
type MultiProxy struct {
	*proxyCore // Embedded
	transport              string
	path                   string
	clients                map[string]*Client
	mcps                   []MultiMCPConfig
	ctx                    context.Context
	mu                     sync.RWMutex        // protects registeredTools, registeredRes, registeredResTemplates, registeredPropts
	registeredTools        map[string][]string // client name -> tool names
	registeredRes          map[string][]string // client name -> resource URIs
	registeredResTemplates map[string][]string // client name -> resource template URIs
	registeredPropts       map[string][]string // client name -> prompt names
}

// NewMultiProxy creates a new multi-proxy
func NewMultiProxy(
	transport string,
	clients map[string]*Client,
	mcps []MultiMCPConfig,
	path string,
	authenticator *auth.Authenticator,
	authorizer *auth.Authorizer,
) (*MultiProxy, error) {
	core, err := newProxyCore("mcp-auth-multi-proxy", "1.0.0", authenticator, authorizer, transport)
	if err != nil {
		return nil, err
	}

	return &MultiProxy{
		proxyCore:              core,
		transport:              transport,
		path:                   path,
		clients:                clients,
		mcps:                   mcps,
		registeredTools:        make(map[string][]string),
		registeredRes:          make(map[string][]string),
		registeredResTemplates: make(map[string][]string),
		registeredPropts:       make(map[string][]string),
	}, nil
}

// Init initializes the multi-proxy
func (p *MultiProxy) Init(ctx context.Context) error {
	p.ctx = ctx

	// Set up initial handlers and start watching for connection events for each client
	for _, mcpConfig := range p.mcps {
		client, ok := p.clients[mcpConfig.Name]
		if !ok {
			return fmt.Errorf("client not found for mcp config: %s", mcpConfig.Name)
		}

		// If client is already connected, set up handlers immediately
		if initResult := client.GetInitResult(); initResult != nil {
			if err := p.setupProxyHandlers(ctx, client, initResult, mcpConfig.Name, mcpConfig.Prefix); err != nil {
				return fmt.Errorf("failed to setup proxy handlers for client %s: %w", mcpConfig.Name, err)
			}
		}

		// Start a goroutine to watch for connection/reconnection events
		go p.watchClientConnectionEvents(ctx, mcpConfig.Name, mcpConfig.Prefix)
	}

	return nil
}

// watchClientConnectionEvents monitors a specific client for connection/reconnection events
// and updates the proxy handlers accordingly
func (p *MultiProxy) watchClientConnectionEvents(ctx context.Context, clientName, prefix string) {
	client, ok := p.clients[clientName]
	if !ok {
		logrus.Errorf("MultiProxy: client not found: %s", clientName)
		return
	}

	watchClientEvents(ctx, client, func(eventType EventType) error {
		return p.refreshHandlers(p.ctx, client, clientName, prefix, eventType)
	})
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
	// Create strategies for this client
	naming := &multiProxyNamingStrategy{prefix: prefix}
	registry := &multiProxyClientRegistry{
		proxy:      p,
		clientName: clientName,
	}

	return setupProxyHandlersWithStrategy(ctx, p.mcpServer, client, initResult, naming, registry)
}

// refreshHandlers clears and re-registers all handlers for a specific client
func (p *MultiProxy) refreshHandlers(ctx context.Context, client *Client, clientName, prefix string, eventType EventType) error {
	logrus.Infof("MultiProxy: client %s - %s - refreshing handlers", clientName, eventType.String())

	// Clear handlers for this specific client
	p.clearClientHandlers(clientName)

	// Get the current initialization result
	initResult := client.GetInitResult()
	if initResult == nil {
		return fmt.Errorf("initResult is nil")
	}

	// Set up handlers with the updated capabilities
	if err := p.setupProxyHandlers(ctx, client, initResult, clientName, prefix); err != nil {
		return fmt.Errorf("failed to setup handlers: %w", err)
	}

	logrus.Infof("MultiProxy: client %s - handlers refreshed successfully after %s", clientName, eventType.String())
	return nil
}
