package proxy

import (
	"encoding/json"
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
	return config, nil
}
