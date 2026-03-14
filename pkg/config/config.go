package config

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type Config struct {
	Server   ServerConfig               `json:"server"`
	Upstream map[string]UpstreamConfig  `json:"upstream"`
	MCPs     map[string]MCPConfig       `json:"mcps"`
	Users    map[string]UserConfig      `json:"users"`
}

type ServerConfig struct {
	Host             string      `json:"host"`
	Admin            Admin       `json:"admin"`
	Auth             *AuthConfig `json:"auth,omitempty"`
	CORSAllowOrigin  string      `json:"cors_allow_origin,omitempty"`
	AuthCookieSecure *bool       `json:"auth_cookie_secure,omitempty"`
}

// GetAuthCookieSecure returns the AuthCookieSecure value, defaulting to true if not set.
func (s *ServerConfig) GetAuthCookieSecure() bool {
	if s.AuthCookieSecure == nil {
		return true
	}
	return *s.AuthCookieSecure
}

type Admin struct {
	Users  []string `json:"users"`
	Groups []string `json:"groups"`
}

type AuthConfig struct {
	Type                string `json:"type"`                 // "oidc" or "token"
	IssuerURL           string `json:"issuer_url,omitempty"` // OIDC provider URL
	ClientID            string `json:"client_id,omitempty"`  // OAuth client ID
	ClientSecret        string `json:"client_secret,omitempty"`
	RedirectURL         string `json:"redirect_url,omitempty"` // Callback URL
	UserClaimField      string `json:"user_claim_field,omitempty"`
	GroupClaimField     string `json:"group_claim_field,omitempty"`
	RequireUserInConfig bool   `json:"require_user_in_config,omitempty"`
}

type UpstreamAuthConfig struct {
	ClientID string `json:"client_id,omitempty"` // override, skip dynamic registration
}

type UpstreamConfig struct {
	Name           string              `json:"-"`
	Transport      string              `json:"transport"`
	Path           string              `json:"path"`
	Cmd            string              `json:"cmd"`
	CmdArgs        []string            `json:"args"`
	URL            string              `json:"url"`
	Bearer         string              `json:"bearer,omitempty"`
	Auth           *UpstreamAuthConfig `json:"auth,omitempty"`
	PingInterval   time.Duration       `json:"pingInterval"`
	ReconnectDelay time.Duration       `json:"reconnectDelay"`
}

type MCPConfig struct {
	Name      string
	Path      string               `json:"path"`
	Transport string               `json:"transport"`
	Upstream  string               `json:"upstream,omitempty"`
	Upstreams []MultiUpstreamConfig `json:"upstreams,omitempty"`
	Auth      []AuthRule           `json:"auth,omitempty"`
}

type MultiUpstreamConfig struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
}

type AuthRule struct {
	User   string   `json:"user,omitempty"`
	Group  string   `json:"group,omitempty"`
	Method string   `json:"method,omitempty"`
	Allow  []string `json:"allow"`
}

type UserConfig struct {
	Token  string   `json:"token"`
	Groups []string `json:"groups"`
}

// Validate checks the configuration for errors and returns meaningful error messages
func (c *Config) Validate() error {
	// Validate Server configuration
	if err := validateServerConfig(&c.Server); err != nil {
		return err
	}

	// Validate Upstream configurations
	for name, upstream := range c.Upstream {
		if err := validateUpstreamConfig(name, &upstream); err != nil {
			return err
		}
	}

	// Validate MCP configurations
	for name, mcp := range c.MCPs {
		if err := validateMCPConfig(name, &mcp, c.Upstream); err != nil {
			return err
		}
	}

	// Validate User configurations
	for name, user := range c.Users {
		if err := validateUserConfig(name, &user); err != nil {
			return err
		}
	}

	return nil
}

func validateServerConfig(server *ServerConfig) error {
	// Auth configuration is optional
	if server.Auth == nil || server.Auth.Type == "" {
		return nil
	}

	auth := server.Auth

	// Expand environment variables in client_secret
	auth.ClientSecret = expandEnvVars(auth.ClientSecret)

	// Validate OIDC configuration
	if auth.Type == "oidc" {
		if auth.IssuerURL == "" {
			return fmt.Errorf("server.auth: 'issuer_url' is required when type is 'oidc'")
		}

		// Validate issuer_url is a valid URL
		if _, err := url.Parse(auth.IssuerURL); err != nil {
			return fmt.Errorf("server.auth: 'issuer_url' is not a valid URL: %w", err)
		}

		if auth.ClientID == "" {
			return fmt.Errorf("server.auth: 'client_id' is required when type is 'oidc'")
		}

		if auth.ClientSecret == "" {
			return fmt.Errorf("server.auth: 'client_secret' is required when type is 'oidc'")
		}

		if auth.RedirectURL == "" {
			return fmt.Errorf("server.auth: 'redirect_url' is required when type is 'oidc'")
		}

		// Validate redirect_url is a valid URL
		if _, err := url.Parse(auth.RedirectURL); err != nil {
			return fmt.Errorf("server.auth: 'redirect_url' is not a valid URL: %w", err)
		}
	}

	return nil
}

// expandEnvVars expands environment variable references in the format ${VAR_NAME}
func expandEnvVars(s string) string {
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		varName := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		if value := os.Getenv(varName); value != "" {
			return value
		}
		return match // Return original if env var not found
	})
}

func validateUpstreamConfig(name string, upstream *UpstreamConfig) error {
	// Validate transport field
	validTransports := map[string]bool{
		"stdio":          true,
		"streamablehttp": true,
	}

	if !validTransports[upstream.Transport] {
		return fmt.Errorf("upstream '%s': invalid transport '%s', must be one of: stdio, streamablehttp", name, upstream.Transport)
	}

	// Validate stdio transport requirements
	if upstream.Transport == "stdio" {
		if upstream.Cmd == "" {
			return fmt.Errorf("upstream '%s': 'cmd' is required when transport is 'stdio'", name)
		}
		if upstream.URL != "" {
			return fmt.Errorf("upstream '%s': 'url' cannot be set when transport is 'stdio'", name)
		}
	} else {
		// For streamablehttp transport
		if upstream.Cmd != "" {
			return fmt.Errorf("upstream '%s': 'cmd' cannot be set when transport is '%s'", name, upstream.Transport)
		}
		if len(upstream.CmdArgs) > 0 {
			return fmt.Errorf("upstream '%s': 'args' cannot be set when transport is '%s'", name, upstream.Transport)
		}
		if upstream.URL == "" {
			return fmt.Errorf("upstream '%s': 'url' is required when transport is '%s'", name, upstream.Transport)
		}
	}

	// Validate Auth is only used with streamablehttp
	if upstream.Auth != nil && upstream.Transport != "streamablehttp" {
		return fmt.Errorf("upstream '%s': 'auth' is only allowed when transport is 'streamablehttp'", name)
	}

	return nil
}

func validateMCPConfig(name string, mcp *MCPConfig, upstreamConfigs map[string]UpstreamConfig) error {
	// Check that exactly one of Upstream or Upstreams is set
	upstreamSet := mcp.Upstream != ""
	upstreamsSet := len(mcp.Upstreams) > 0

	if !upstreamSet && !upstreamsSet {
		return fmt.Errorf("mcp '%s': either 'upstream' or 'upstreams' must be set", name)
	}
	if upstreamSet && upstreamsSet {
		return fmt.Errorf("mcp '%s': cannot set both 'upstream' and 'upstreams', only one is allowed", name)
	}

	// Validate Upstream reference
	if upstreamSet {
		if _, exists := upstreamConfigs[mcp.Upstream]; !exists {
			return fmt.Errorf("mcp '%s': upstream '%s' is not defined in the upstream section", name, mcp.Upstream)
		}
	}

	// Validate Upstreams references
	if upstreamsSet {
		for _, multiUpstream := range mcp.Upstreams {
			if _, exists := upstreamConfigs[multiUpstream.Name]; !exists {
				return fmt.Errorf("mcp '%s': upstream '%s' (in upstreams) is not defined in the upstream section", name, multiUpstream.Name)
			}
		}
	}

	return nil
}

func validateUserConfig(name string, user *UserConfig) error {
	// Check for duplicate group names within a user
	groupSet := make(map[string]bool)
	for _, group := range user.Groups {
		if groupSet[group] {
			return fmt.Errorf("user '%s': duplicate group '%s' found", name, group)
		}
		groupSet[group] = true
	}

	return nil
}

func ReadConfig(path string) (*Config, error) {
	configFile, err := os.Open(path)
	defer func() {
		if configFile == nil {
			return
		}
		if err := configFile.Close(); err != nil {
			log.Printf("Error closing config file: %v", err)
		}
	}()
	if err != nil {
		log.Fatal(err)
	}
	jsonParser := json.NewDecoder(configFile)
	config := &Config{}
	if err := jsonParser.Decode(config); err != nil {
		return nil, err
	}
	for name := range config.Upstream {
		upstream := config.Upstream[name]
		upstream.Name = name
		config.Upstream[name] = upstream
	}
	for name := range config.MCPs {
		mcp := config.MCPs[name]
		mcp.Name = name
		config.MCPs[name] = mcp
	}

	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}
