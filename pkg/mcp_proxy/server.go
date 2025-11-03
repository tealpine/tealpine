package mcp_proxy

import (
	"context"
	"fmt"
	"log"
	"net/http"
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
		if _, err := client.Init(ctx); err != nil {
			return fmt.Errorf("failed to initialize client %s: %w", name, err)
		}
	}

	// 2. Create and initialize all proxies
	for name, proxyConfig := range s.cfg.Proxy {
		var proxy http.Handler
		if proxyConfig.MCP != "" { // Single Proxy
			client, ok := s.clients[proxyConfig.MCP]
			if !ok {
				return fmt.Errorf("client not found for proxy %s: %s", name, proxyConfig.MCP)
			}
			singleProxy := NewSingleProxy(proxyConfig.Transport, client, proxyConfig.Path)
			if err := singleProxy.Init(ctx); err != nil {
				return fmt.Errorf("failed to initialize single proxy %s: %w", name, err)
			}
			proxy = singleProxy
		} else if len(proxyConfig.MCPs) > 0 { // Multi Proxy
			multiProxy := NewMultiProxy(proxyConfig.Transport, s.clients, proxyConfig.MCPs, proxyConfig.Path)
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
