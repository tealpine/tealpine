package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

type Config struct {
	Server ServerConfig           `json:"server"`
	MCP    map[string]MCPConfig   `json:"mcp"`
	Proxy  map[string]ProxyConfig `json:"proxy"`
	Users  map[string]UserConfig  `json:"users"`
}

type ServerConfig struct {
	Host string `json:"host"`
}

type MCPConfig struct {
	Name           string        `json:"-"`
	Transport      string        `json:"transport"`
	Path           string        `json:"path"`
	Cmd            string        `json:"cmd"`
	CmdArgs        []string      `json:"args"`
	URL            string        `json:"url"`
	PingInterval   time.Duration `json:"pingInterval"`
	ReconnectDelay time.Duration `json:"reconnectDelay"`
}

type ProxyConfig struct {
	Name      string
	Path      string           `json:"path"`
	Transport string           `json:"transport"`
	MCP       string           `json:"mcp,omitempty"`
	MCPs      []MultiMCPConfig `json:"mcps,omitempty"`
	Auth      []AuthRule       `json:"auth,omitempty"`
}

type MultiMCPConfig struct {
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
	// Validate MCP configurations
	for name, mcp := range c.MCP {
		if err := validateMCPConfig(name, &mcp); err != nil {
			return err
		}
	}

	// Validate Proxy configurations
	for name, proxy := range c.Proxy {
		if err := validateProxyConfig(name, &proxy, c.MCP); err != nil {
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

func validateMCPConfig(name string, mcp *MCPConfig) error {
	// Validate transport field
	validTransports := map[string]bool{
		"stdio":           true,
		"sse":             true,
		"streamablehttp":  true,
	}

	if !validTransports[mcp.Transport] {
		return fmt.Errorf("mcp '%s': invalid transport '%s', must be one of: stdio, sse, streamablehttp", name, mcp.Transport)
	}

	// Validate stdio transport requirements
	if mcp.Transport == "stdio" {
		if mcp.Cmd == "" {
			return fmt.Errorf("mcp '%s': 'cmd' is required when transport is 'stdio'", name)
		}
		if mcp.URL != "" {
			return fmt.Errorf("mcp '%s': 'url' cannot be set when transport is 'stdio'", name)
		}
	} else {
		// For sse and streamablehttp transports
		if mcp.Cmd != "" {
			return fmt.Errorf("mcp '%s': 'cmd' cannot be set when transport is '%s'", name, mcp.Transport)
		}
		if len(mcp.CmdArgs) > 0 {
			return fmt.Errorf("mcp '%s': 'args' cannot be set when transport is '%s'", name, mcp.Transport)
		}
		if mcp.URL == "" {
			return fmt.Errorf("mcp '%s': 'url' is required when transport is '%s'", name, mcp.Transport)
		}
	}

	return nil
}

func validateProxyConfig(name string, proxy *ProxyConfig, mcpConfigs map[string]MCPConfig) error {
	// Check that exactly one of MCP or MCPs is set
	mcpSet := proxy.MCP != ""
	mcpsSet := len(proxy.MCPs) > 0

	if !mcpSet && !mcpsSet {
		return fmt.Errorf("proxy '%s': either 'mcp' or 'mcps' must be set", name)
	}
	if mcpSet && mcpsSet {
		return fmt.Errorf("proxy '%s': cannot set both 'mcp' and 'mcps', only one is allowed", name)
	}

	// Validate MCP reference
	if mcpSet {
		if _, exists := mcpConfigs[proxy.MCP]; !exists {
			return fmt.Errorf("proxy '%s': mcp '%s' is not defined in the mcp section", name, proxy.MCP)
		}
	}

	// Validate MCPs references
	if mcpsSet {
		for _, multiMCP := range proxy.MCPs {
			if _, exists := mcpConfigs[multiMCP.Name]; !exists {
				return fmt.Errorf("proxy '%s': mcp '%s' (in mcps) is not defined in the mcp section", name, multiMCP.Name)
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
	for name := range config.MCP {
		mcp := config.MCP[name]
		mcp.Name = name
		config.MCP[name] = mcp
	}
	for name := range config.Proxy {
		proxy := config.Proxy[name]
		proxy.Name = name
		config.Proxy[name] = proxy
	}

	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}
