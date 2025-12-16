package server

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"mcp-auth-proxy/pkg/auth"
	"mcp-auth-proxy/pkg/client"
	"mcp-auth-proxy/pkg/config"
	"mcp-auth-proxy/pkg/proxy"
)

type Server struct {
	cfg        *config.Config
	clients    map[string]*client.Client
	proxies    map[string]http.Handler
	httpServer *http.Server
}

func NewServer(cfg *config.Config) *Server {
	return &Server{
		cfg:     cfg,
		clients: make(map[string]*client.Client),
		proxies: make(map[string]http.Handler),
	}
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
		// Build users map for authentication
		users := make(map[string]*auth.UserInfo)
		for username, userConfig := range s.cfg.Users {
			users[username] = &auth.UserInfo{
				Token:  userConfig.Token,
				Groups: userConfig.Groups,
			}
		}

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
		authenticator := auth.NewAuthenticator(users)

		// Create authorizer (MCP middleware for Casbin enforcement)
		authorizer, err := auth.NewAuthorizer(users, authRules)
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

	// 3. Create http server
	mux := http.NewServeMux()
	for path, proxyHandler := range s.proxies {
		mux.Handle("/"+path+"/", proxyHandler)
	}

	s.httpServer = &http.Server{
		Addr:    s.cfg.Server.Host,
		Handler: mux,
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

func (s *Server) Run() error {
	log.Printf("Starting server on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
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
