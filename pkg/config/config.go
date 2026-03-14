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
	Server   ServerConfig              `json:"server"`
	Upstream map[string]UpstreamConfig `json:"upstream"`
	MCPs     map[string]MCPConfig      `json:"mcps"`
	Groups   map[string][]string       `json:"groups"`
	Users    map[string]UserConfig     `json:"users"`
}

type ServerConfig struct {
	Host             string      `json:"host"`
	Admin            []string    `json:"admin,omitempty"`
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
	Path      string                `json:"path"`
	Transport string                `json:"transport"`
	Upstream  string                `json:"upstream,omitempty"`
	Upstreams []MultiUpstreamConfig `json:"upstreams,omitempty"`
	Auth      map[string][]AuthRule `json:"auth,omitempty"`
}

type MultiUpstreamConfig struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
}

// AuthRule is a per-method allow rule. The group it applies to is the map key in MCPConfig.Auth.
type AuthRule struct {
	Method string   `json:"method,omitempty"`
	Allow  []string `json:"allow"`
}

type UserConfig struct {
	Token string `json:"token"`
}

// Validate checks the configuration for errors and returns meaningful error messages
func (c *Config) Validate() error {
	if err := validateServerConfig(&c.Server); err != nil {
		return err
	}

	for name, upstream := range c.Upstream {
		if err := validateUpstreamConfig(name, &upstream); err != nil {
			return err
		}
	}

	for name, mcp := range c.MCPs {
		if err := validateMCPConfig(name, &mcp, c.Upstream); err != nil {
			return err
		}
	}

	if err := validateGroupsConfig(c.Groups, c.Users); err != nil {
		return err
	}

	if err := validateAdminGroups(c.Server.Admin, c.Groups); err != nil {
		return err
	}

	for name, mcp := range c.MCPs {
		if err := validateMCPAuthGroups(name, mcp.Auth, c.Groups); err != nil {
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
		return match
	})
}

func validateUpstreamConfig(name string, upstream *UpstreamConfig) error {
	validTransports := map[string]bool{
		"stdio":          true,
		"streamablehttp": true,
	}

	if !validTransports[upstream.Transport] {
		return fmt.Errorf("upstream '%s': invalid transport '%s', must be one of: stdio, streamablehttp", name, upstream.Transport)
	}

	if upstream.Transport == "stdio" {
		if upstream.Cmd == "" {
			return fmt.Errorf("upstream '%s': 'cmd' is required when transport is 'stdio'", name)
		}
		if upstream.URL != "" {
			return fmt.Errorf("upstream '%s': 'url' cannot be set when transport is 'stdio'", name)
		}
	} else {
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

	if upstream.Auth != nil && upstream.Transport != "streamablehttp" {
		return fmt.Errorf("upstream '%s': 'auth' is only allowed when transport is 'streamablehttp'", name)
	}

	return nil
}

func validateMCPConfig(name string, mcp *MCPConfig, upstreamConfigs map[string]UpstreamConfig) error {
	upstreamSet := mcp.Upstream != ""
	upstreamsSet := len(mcp.Upstreams) > 0

	if !upstreamSet && !upstreamsSet {
		return fmt.Errorf("mcp '%s': either 'upstream' or 'upstreams' must be set", name)
	}
	if upstreamSet && upstreamsSet {
		return fmt.Errorf("mcp '%s': cannot set both 'upstream' and 'upstreams', only one is allowed", name)
	}

	if upstreamSet {
		if _, exists := upstreamConfigs[mcp.Upstream]; !exists {
			return fmt.Errorf("mcp '%s': upstream '%s' is not defined in the upstream section", name, mcp.Upstream)
		}
	}

	if upstreamsSet {
		for _, multiUpstream := range mcp.Upstreams {
			if _, exists := upstreamConfigs[multiUpstream.Name]; !exists {
				return fmt.Errorf("mcp '%s': upstream '%s' (in upstreams) is not defined in the upstream section", name, multiUpstream.Name)
			}
		}
	}

	return nil
}

// validateGroupsConfig validates that all users referenced in groups exist in the users section
func validateGroupsConfig(groups map[string][]string, users map[string]UserConfig) error {
	for groupName, members := range groups {
		for _, username := range members {
			if _, exists := users[username]; !exists {
				return fmt.Errorf("group '%s': user '%s' is not defined in the users section", groupName, username)
			}
		}
	}
	return nil
}

// validateAdminGroups validates that all groups in server.admin are defined in the groups section
func validateAdminGroups(adminGroups []string, groups map[string][]string) error {
	for _, group := range adminGroups {
		if _, exists := groups[group]; !exists {
			return fmt.Errorf("server.admin: group '%s' is not defined in the groups section", group)
		}
	}
	return nil
}

// validateMCPAuthGroups validates that all groups referenced in an MCP's auth are defined in the groups section
func validateMCPAuthGroups(mcpName string, auth map[string][]AuthRule, groups map[string][]string) error {
	for groupName := range auth {
		if _, exists := groups[groupName]; !exists {
			return fmt.Errorf("mcp '%s': auth group '%s' is not defined in the groups section", mcpName, groupName)
		}
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

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}
