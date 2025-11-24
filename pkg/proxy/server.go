package proxy

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"mcp-auth-proxy/pkg/auth"
)

type Server struct {
	cfg        *Config
	clients    map[string]*Client
	proxies    map[string]http.Handler
	httpServer *http.Server
}

func NewServer(cfg *Config) *Server {
	return &Server{
		cfg:     cfg,
		clients: make(map[string]*Client),
		proxies: make(map[string]http.Handler),
	}
}

func (s *Server) Init(ctx context.Context) error {
	// 1. Create and initialize all mcp clients
	for name, mcpConfig := range s.cfg.MCP {
		client := NewClient(mcpConfig)
		s.clients[name] = client
		client.Start(ctx)
	}

	// 2. Create and initialize all proxies
	for name, proxyConfig := range s.cfg.Proxy {
		// Create auth middleware for this proxy
		users := make(map[string]*auth.UserInfo)
		for username, userConfig := range s.cfg.Users {
			users[username] = &auth.UserInfo{
				Token:  userConfig.Token,
				Groups: userConfig.Groups,
			}
		}

		authRules := make([]auth.AuthRule, 0, len(proxyConfig.Auth))
		for _, rule := range proxyConfig.Auth {
			authRules = append(authRules, auth.AuthRule{
				User:   rule.User,
				Group:  rule.Group,
				Method: rule.Method,
				Allow:  rule.Allow,
			})
		}

		authMiddleware, err := auth.NewAuth(users, authRules)
		if err != nil {
			return fmt.Errorf("failed to create auth middleware for proxy %s: %w", name, err)
		}

		var proxy http.Handler
		if proxyConfig.MCP != "" { // Single Proxy
			client, ok := s.clients[proxyConfig.MCP]
			if !ok {
				return fmt.Errorf("client not found for proxy %s: %s", name, proxyConfig.MCP)
			}
			singleProxy := NewSingleProxy(proxyConfig.Transport, client, proxyConfig.Path, authMiddleware)
			if err := singleProxy.Init(ctx); err != nil {
				return fmt.Errorf("failed to initialize single proxy %s: %w", name, err)
			}
			proxy = singleProxy
		} else if len(proxyConfig.MCPs) > 0 { // Multi Proxy
			multiProxy := NewMultiProxy(proxyConfig.Transport, s.clients, proxyConfig.MCPs, proxyConfig.Path, authMiddleware)
			if err := multiProxy.Init(ctx); err != nil {
				return fmt.Errorf("failed to initialize multi proxy %s: %w", name, err)
			}
			proxy = multiProxy
		} else {
			return fmt.Errorf("proxy %s has no mcp or mcps configuration", name)
		}
		s.proxies[proxyConfig.Path] = proxy
	}

	// 3. Create http server
	mux := http.NewServeMux()
	for path, proxy := range s.proxies {
		mux.Handle("/"+path+"/", proxy)
	}

	s.httpServer = &http.Server{
		Addr:    s.cfg.Server.Host,
		Handler: mux,
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
	for name, client := range s.clients {
		if err := client.Close(); err != nil {
			log.Printf("Error closing client %s: %v", name, err)
		}
	}
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}
