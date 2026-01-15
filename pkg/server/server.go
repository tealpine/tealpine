package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"tealpine/pkg/auth"
	"tealpine/pkg/client"
	"tealpine/pkg/config"
	"tealpine/pkg/proxy"

	"github.com/gin-gonic/gin"
)

type Server struct {
	cfg        *config.Config
	clients    map[string]*client.Client
	proxies    map[string]http.Handler
	ginEngine  *gin.Engine
	httpServer *http.Server
}

func NewServer(cfg *config.Config) *Server {
	return &Server{
		cfg:     cfg,
		clients: make(map[string]*client.Client),
		proxies: make(map[string]http.Handler),
	}
}

// statusHandler returns the connection status of all clients
func (s *Server) statusHandler(c *gin.Context) {
	type ClientStatus struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	statuses := make([]ClientStatus, 0, len(s.clients))
	for name, client := range s.clients {
		status := "disconnected"
		if client.IsConnected() {
			status = "connected"
		}
		statuses = append(statuses, ClientStatus{
			Name:   name,
			Status: status,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"clients": statuses,
	})
}

func (s *Server) Init(ctx context.Context) error {
	// 1. Create and initialize all mcp clients
	for name, mcpConfig := range s.cfg.MCP {
		c := client.NewClient(mcpConfig)
		s.clients[name] = c
		c.Start(ctx)
	}

	// 2. Create and initialize all proxies
	for name, proxyConfig := range s.cfg.Proxy {
		// Build auth rules for authorization
		authRules := make([]auth.AuthRule, 0, len(proxyConfig.Auth))
		for _, rule := range proxyConfig.Auth {
			authRules = append(authRules, auth.AuthRule{
				User:   rule.User,
				Group:  rule.Group,
				Method: rule.Method,
				Allow:  rule.Allow,
			})
		}

		// Create authenticator (HTTP middleware for token validation)
		authenticator := auth.NewAuthenticator(s.cfg.Users, s.cfg.Server.Auth)

		// Create authorizer (MCP middleware for Casbin enforcement)
		authorizer, err := auth.NewAuthorizer(s.cfg.Users, authRules)
		if err != nil {
			return fmt.Errorf("failed to create authorizer for proxy %s: %w", name, err)
		}

		var proxyHandler http.Handler
		if proxyConfig.MCP != "" { // Single Proxy
			c, ok := s.clients[proxyConfig.MCP]
			if !ok {
				return fmt.Errorf("client not found for proxy %s: %s", name, proxyConfig.MCP)
			}
			singleProxy, err := proxy.NewSingleProxy(proxyConfig.Transport, c, proxyConfig.Path, authenticator, authorizer)
			if err != nil {
				return fmt.Errorf("failed to create single proxy %s: %w", name, err)
			}
			if err := singleProxy.Init(ctx); err != nil {
				return fmt.Errorf("failed to initialize single proxy %s: %w", name, err)
			}
			proxyHandler = singleProxy
		} else if len(proxyConfig.MCPs) > 0 { // Multi Proxy
			multiProxy, err := proxy.NewMultiProxy(proxyConfig.Transport, s.clients, proxyConfig.MCPs, proxyConfig.Path, authenticator, authorizer)
			if err != nil {
				return fmt.Errorf("failed to create multi proxy %s: %w", name, err)
			}
			if err := multiProxy.Init(ctx); err != nil {
				return fmt.Errorf("failed to initialize multi proxy %s: %w", name, err)
			}
			proxyHandler = multiProxy
		} else {
			return fmt.Errorf("proxy %s has no mcp or mcps configuration", name)
		}
		s.proxies[proxyConfig.Path] = proxyHandler
	}

	// 3. Create Gin engine and http server
	s.ginEngine = gin.New()
	s.ginEngine.Use(gin.Recovery())
	s.ginEngine.Use(CORSMiddleware())

	// Create authenticator and admin authorizer for /tealpine endpoints
	authenticator := auth.NewAuthenticator(s.cfg.Users, s.cfg.Server.Auth)
	adminAuthorizer := auth.NewAdminAuthorizer(s.cfg.Users, s.cfg.Server.Admin.Users, s.cfg.Server.Admin.Groups)

	// Register OAuth endpoints if OIDC is enabled
	if s.cfg.Server.Auth != nil && s.cfg.Server.Auth.Type == "oidc" {
		oauthHandlers := NewOAuthHandlers(authenticator)
		// Use Any() to handle all HTTP methods including OPTIONS for CORS preflight
		s.ginEngine.Any("/auth/login", oauthHandlers.HandleLogin)
		s.ginEngine.Any("/auth/callback", oauthHandlers.HandleCallback)
		s.ginEngine.Any("/auth/logout", oauthHandlers.HandleLogout)

		// Register OAuth 2.0 Protected Resource Metadata endpoints (RFC 9728)
		// Required by MCP specification for client discovery
		// Use Any() to handle all HTTP methods including OPTIONS for CORS preflight
		metadataHandlers := NewMetadataHandlers(s.cfg.Server.Auth, s.cfg.Server.Host)
		s.ginEngine.Any("/.well-known/oauth-protected-resource", metadataHandlers.HandleProtectedResourceMetadata)
		s.ginEngine.Any("/.well-known/oauth-protected-resource/*path", metadataHandlers.HandlePathSpecificMetadata)
		s.ginEngine.Any("/.well-known/openid-configuration", metadataHandlers.HandleOpenIDConfiguration)
	}

	// Convert HTTP middleware to Gin middleware
	adminMiddleware := func(c *gin.Context) {
		// Create a handler that will be wrapped by the auth middlewares
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Update the gin context with the modified request (contains username in context)
			c.Request = r
			c.Next()
		})

		// Apply authentication and authorization
		authenticator.Middleware(adminAuthorizer.Middleware(handler)).ServeHTTP(c.Writer, c.Request)
	}

	// Register admin API endpoints with authentication and authorization
	tealpineGroup := s.ginEngine.Group("/tealpine")
	tealpineGroup.Use(adminMiddleware)
	tealpineGroup.GET("/api/v1/status", s.statusHandler)

	// Register proxy endpoints
	for path, proxyHandler := range s.proxies {
		s.ginEngine.Any("/"+path+"/*proxyPath", gin.WrapH(proxyHandler))
	}

	s.httpServer = &http.Server{
		Addr:    s.cfg.Server.Host,
		Handler: s.ginEngine,
	}

	return nil
}

func (s *Server) WaitForClients(ctx context.Context) error {
	for name, c := range s.clients {
		if err := c.WaitForConnection(ctx); err != nil {
			return fmt.Errorf("failed to wait for client %s to connect: %w", name, err)
		}
	}
	return nil
}

func (s *Server) GetHTTPServer() *http.Server {
	return s.httpServer
}

func (s *Server) Run() error {
	log.Printf("Starting server on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	// Close clients
	for name, c := range s.clients {
		if err := c.Close(); err != nil {
			log.Printf("Error closing client %s: %v", name, err)
		}
	}
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}
