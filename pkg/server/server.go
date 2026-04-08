package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"

	"tealpine/pkg/auth"
	"tealpine/pkg/client"
	"tealpine/pkg/config"
	"tealpine/pkg/proxy"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type Server struct {
	cfg            *config.Config
	clients        map[string]*client.Client
	proxies        map[string]http.Handler
	tokenStore     *auth.TokenStore
	sessionStore   *auth.SessionStore
	ginEngine      *gin.Engine
	httpServer     *http.Server
	cancelRequests context.CancelFunc // cancels all active HTTP request contexts
}

func NewServer(cfg *config.Config, tokensPath string) *Server {
	return &Server{
		cfg:          cfg,
		clients:      make(map[string]*client.Client),
		proxies:      make(map[string]http.Handler),
		tokenStore:   auth.NewTokenStore(tokensPath),
		sessionStore: auth.NewSessionStore(),
	}
}

// statusHandler returns the connection status of all clients
func (s *Server) statusHandler(c *gin.Context) {
	type ClientStatus struct {
		Name     string `json:"name"`
		Status   string `json:"status"`
		LoginURL string `json:"login_url,omitempty"`
	}

	statuses := make([]ClientStatus, 0, len(s.clients))
	for name, cl := range s.clients {
		status := "disconnected"
		var loginURL string
		if cl.IsConnected() {
			status = "connected"
		} else if cl.IsLoginRequired() {
			status = "login_required"
			loginURL = "/upstream/" + name + "/auth/login"
		}
		statuses = append(statuses, ClientStatus{
			Name:     name,
			Status:   status,
			LoginURL: loginURL,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"clients": statuses,
	})
}

func (s *Server) Init(ctx context.Context) error {
	// 0. Load persisted tokens and inject bearer tokens into MCP configs
	tokens, err := s.tokenStore.Load()
	if err != nil {
		logrus.Warnf("failed to load tokens: %v", err)
	} else {
		for name, token := range tokens {
			if upstreamCfg, ok := s.cfg.Upstream[name]; ok && token.AccessToken != "" {
				upstreamCfg.Bearer = token.AccessToken
				s.cfg.Upstream[name] = upstreamCfg
				logrus.Infof("loaded persisted token for upstream '%s'", name)
			}
		}
	}

	// 1. Create and initialize all upstream clients
	for name, upstreamConfig := range s.cfg.Upstream {
		c := client.NewClient(upstreamConfig)
		s.clients[name] = c
		c.Start(ctx)
	}

	// 2. Create and initialize all MCPs
	for name, mcpConfig := range s.cfg.MCPs {
		// Build auth rules for authorization
		authRules := make([]auth.AuthRule, 0)
		for groupName, rules := range mcpConfig.Auth {
			for _, rule := range rules {
				authRules = append(authRules, auth.AuthRule{
					Group:  groupName,
					Method: rule.Method,
					Allow:  rule.Allow,
				})
			}
		}

		// Create authenticator (HTTP middleware for token validation)
		authenticator := auth.NewAuthenticator(s.cfg.Users, s.cfg.Server.Auth, s.cfg.Server.GetRedirectURL())

		// Create authorizer (MCP middleware for Casbin enforcement)
		authorizer, err := auth.NewAuthorizer(s.cfg.Groups, authRules)
		if err != nil {
			return fmt.Errorf("failed to create authorizer for mcp %s: %w", name, err)
		}

		var proxyHandler http.Handler
		if mcpConfig.Upstream != "" { // Single Proxy
			c, ok := s.clients[mcpConfig.Upstream]
			if !ok {
				return fmt.Errorf("client not found for mcp %s: %s", name, mcpConfig.Upstream)
			}
			singleProxy, err := proxy.NewSingleProxy(mcpConfig.Transport, c, mcpConfig.Path, authenticator, authorizer)
			if err != nil {
				return fmt.Errorf("failed to create single proxy %s: %w", name, err)
			}
			if err := singleProxy.Init(ctx); err != nil {
				return fmt.Errorf("failed to initialize single proxy %s: %w", name, err)
			}
			proxyHandler = singleProxy
		} else if len(mcpConfig.Upstreams) > 0 { // Multi Proxy
			multiProxy, err := proxy.NewMultiProxy(mcpConfig.Transport, s.clients, mcpConfig.Upstreams, mcpConfig.Path, authenticator, authorizer)
			if err != nil {
				return fmt.Errorf("failed to create multi proxy %s: %w", name, err)
			}
			if err := multiProxy.Init(ctx); err != nil {
				return fmt.Errorf("failed to initialize multi proxy %s: %w", name, err)
			}
			proxyHandler = multiProxy
		} else {
			return fmt.Errorf("mcp %s has no upstream or upstreams configuration", name)
		}
		s.proxies[mcpConfig.Path] = proxyHandler
	}

	// 3. Create Gin engine and http server
	s.ginEngine = gin.New()
	s.ginEngine.Use(gin.Recovery())
	s.ginEngine.Use(CORSMiddleware(s.cfg.Server.CORSAllowOrigin))

	// Create authenticator and admin authorizer for /tealpine endpoints
	authenticator := auth.NewAuthenticator(s.cfg.Users, s.cfg.Server.Auth, s.cfg.Server.GetRedirectURL())
	adminAuthorizer := auth.NewAdminAuthorizer(s.cfg.Groups, s.cfg.Server.Admin)

	// Register OAuth endpoints if OIDC is enabled
	if s.cfg.Server.Auth != nil && s.cfg.Server.Auth.Type == "oidc" {
		oauthHandlers := NewOAuthHandlers(authenticator, s.cfg.Server.GetAuthCookieSecure())
		// Use Any() to handle all HTTP methods including OPTIONS for CORS preflight
		s.ginEngine.Any("/auth/login", oauthHandlers.HandleLogin)
		s.ginEngine.Any("/auth/callback", oauthHandlers.HandleCallback)
		s.ginEngine.Any("/auth/logout", oauthHandlers.HandleLogout)

		// Register OAuth 2.0 Protected Resource Metadata endpoints (RFC 9728)
		// Required by MCP specification for client discovery
		// Use Any() to handle all HTTP methods including OPTIONS for CORS preflight
		metadataHandlers := NewMetadataHandlers(s.cfg.Server.Auth, s.cfg.Server.Host, s.cfg.Server.GetAuthCookieSecure())
		s.ginEngine.Any("/.well-known/oauth-protected-resource", metadataHandlers.HandleProtectedResourceMetadata)
		s.ginEngine.Any("/.well-known/oauth-protected-resource/*path", metadataHandlers.HandlePathSpecificMetadata)
		s.ginEngine.Any("/.well-known/openid-configuration", metadataHandlers.HandleOpenIDConfiguration)
	}

	// Convert HTTP middleware to Gin middleware
	adminMiddleware := func(c *gin.Context) {
		// Track whether the inner handler was reached (auth passed)
		authPassed := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authPassed = true
			c.Request = r
			c.Next()
		})

		// Apply authentication and authorization
		authenticator.Middleware(adminAuthorizer.Middleware(handler)).ServeHTTP(c.Writer, c.Request)

		// If auth middleware rejected the request (wrote 401), abort the Gin chain
		// to prevent Gin from trying to write a 200 over the already-written headers
		if !authPassed {
			c.Abort()
		}
	}

	// Register admin API endpoints with authentication and authorization
	tealpineGroup := s.ginEngine.Group("/tealpine")
	tealpineGroup.Use(adminMiddleware)
	tealpineGroup.GET("/api/v1/status", s.statusHandler)

	// Register upstream OAuth endpoints for MCP OIDC login
	upstreamHandlers := NewUpstreamOAuthHandlers(s.clients, s.tokenStore, s.sessionStore, s.cfg.Server.Host, s.cfg.Server.GetAuthCookieSecure())
	s.ginEngine.GET("/upstream/:name/auth/login", upstreamHandlers.HandleLogin)
	s.ginEngine.GET("/upstream/:name/auth/callback", upstreamHandlers.HandleCallback)

	// Register proxy endpoints
	for path, proxyHandler := range s.proxies {
		s.ginEngine.Any("/"+path+"/*proxyPath", gin.WrapH(proxyHandler))
	}

	// Create a cancellable context for all HTTP requests.
	// Cancelling it terminates long-lived connections (SSE streams)
	// so httpServer.Shutdown can complete promptly.
	reqCtx, reqCancel := context.WithCancel(context.Background())
	s.cancelRequests = reqCancel

	s.httpServer = &http.Server{
		Addr:    s.cfg.Server.Host,
		Handler: s.ginEngine,
		BaseContext: func(_ net.Listener) context.Context {
			return reqCtx
		},
	}

	return nil
}

func (s *Server) WaitForClients(ctx context.Context) error {
	for name, c := range s.clients {
		// Skip clients that need upstream login — they'll connect after login
		if c.IsLoginRequired() {
			logrus.Infof("client '%s' requires upstream login, skipping wait", name)
			continue
		}
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
	// 1. Cancel all active request contexts (terminates SSE streams)
	if s.cancelRequests != nil {
		s.cancelRequests()
	}

	// 2. Shutdown HTTP server — stops accepting new connections
	// and waits for active handlers to finish (they should exit
	// promptly now that their contexts are cancelled)
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down HTTP server: %v", err)
		}
	}

	// 2. Close clients after proxy handlers have returned
	for name, c := range s.clients {
		if err := c.Close(); err != nil {
			log.Printf("Error closing client %s: %v", name, err)
		}
	}
	return nil
}
