package mcp_proxy

import (
	"encoding/json"
	"log"
	"os"
)

type Config struct {
	Server ServerConfig `json:"server"`
	MCP    []MCPConfig  `json:"mcp"`
}

type ServerConfig struct {
	Host string `json:"host"`
}

type MCPConfig struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Path      string   `json:"path"`
	Cmd       string   `json:"cmd"`
	CmdArgs   []string `json:"args"`
	URL       string   `json:"url"`
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
	return config, nil
}
