package config

import (
	"os"
	"strings"
	"testing"
)

func TestValidateUpstreamConfig_InvalidTransport(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "invalid",
				URL:       "http://localhost:8080",
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for invalid transport, got nil")
	}
	if !strings.Contains(err.Error(), "invalid transport 'invalid'") {
		t.Errorf("unexpected error message: %v", err)
	}
	if !strings.Contains(err.Error(), "must be one of: stdio, streamablehttp") {
		t.Errorf("error should list valid transports: %v", err)
	}
}

func TestValidateUpstreamConfig_StdioMissingCmd(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "stdio",
				// Cmd is missing
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for stdio without cmd, got nil")
	}
	if !strings.Contains(err.Error(), "'cmd' is required when transport is 'stdio'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateUpstreamConfig_StdioWithURL(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "stdio",
				Cmd:       "go",
				URL:       "http://localhost:8080",
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for stdio with url, got nil")
	}
	if !strings.Contains(err.Error(), "'url' cannot be set when transport is 'stdio'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateUpstreamConfig_StdioValid(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "stdio",
				Cmd:       "go",
				CmdArgs:   []string{"run", "main.go"},
			},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name:     "p1",
				Upstream: "test",
			},
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("expected no error for valid stdio config, got: %v", err)
	}
}

func TestValidateUpstreamConfig_StreamableHTTPMissingURL(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "streamablehttp",
				// URL is missing
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for sse without url, got nil")
	}
	if !strings.Contains(err.Error(), "'url' is required when transport is 'streamablehttp'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateUpstreamConfig_StreamableHTTPWithCmd(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
				Cmd:       "go",
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for sse with cmd, got nil")
	}
	if !strings.Contains(err.Error(), "'cmd' cannot be set when transport is 'streamablehttp'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateUpstreamConfig_StreamableHTTPWithArgs(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
				CmdArgs:   []string{"arg1"},
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for sse with args, got nil")
	}
	if !strings.Contains(err.Error(), "'args' cannot be set when transport is 'streamablehttp'") {
		t.Errorf("unexpected error message: %v", err)
	}
}


func TestValidateMCPConfig_BothUpstreamAndUpstreamsEmpty(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
			},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name: "p1",
				// Both Upstream and Upstreams are empty
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for mcp without upstream or upstreams, got nil")
	}
	if !strings.Contains(err.Error(), "either 'upstream' or 'upstreams' must be set") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateMCPConfig_BothUpstreamAndUpstreamsSet(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test1": {
				Name:      "test1",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
			},
			"test2": {
				Name:      "test2",
				Transport: "streamablehttp",
				URL:       "http://localhost:8081",
			},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name:     "p1",
				Upstream: "test1",
				Upstreams: []MultiUpstreamConfig{
					{Name: "test2", Prefix: "t2"},
				},
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for mcp with both upstream and upstreams set, got nil")
	}
	if !strings.Contains(err.Error(), "cannot set both 'upstream' and 'upstreams'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateMCPConfig_UpstreamNotDefined(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
			},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name:     "p1",
				Upstream: "nonexistent",
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for mcp referencing undefined upstream, got nil")
	}
	if !strings.Contains(err.Error(), "upstream 'nonexistent' is not defined in the upstream section") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateMCPConfig_UpstreamsNotDefined(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test1": {
				Name:      "test1",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
			},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name: "p1",
				Upstreams: []MultiUpstreamConfig{
					{Name: "test1", Prefix: "t1"},
					{Name: "nonexistent", Prefix: "ne"},
				},
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for mcp referencing undefined upstream in upstreams, got nil")
	}
	if !strings.Contains(err.Error(), "upstream 'nonexistent' (in upstreams) is not defined in the upstream section") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateMCPConfig_ValidSingleUpstream(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
			},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name:     "p1",
				Upstream: "test",
			},
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("expected no error for valid mcp with single upstream, got: %v", err)
	}
}

func TestValidateMCPConfig_ValidMultipleUpstreams(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test1": {
				Name:      "test1",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
			},
			"test2": {
				Name:      "test2",
				Transport: "streamablehttp",
				URL:       "http://localhost:8081",
			},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name: "p1",
				Upstreams: []MultiUpstreamConfig{
					{Name: "test1", Prefix: "t1"},
					{Name: "test2", Prefix: "t2"},
				},
			},
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("expected no error for valid mcp with multiple upstreams, got: %v", err)
	}
}

func TestValidateUserConfig_DuplicateGroups(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
			},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name:     "p1",
				Upstream: "test",
			},
		},
		Users: map[string]UserConfig{
			"alice": {
				Token:  "token123",
				Groups: []string{"group1", "group2", "group1"},
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for user with duplicate groups, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate group 'group1'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateUserConfig_UniqueGroups(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
			},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name:     "p1",
				Upstream: "test",
			},
		},
		Users: map[string]UserConfig{
			"alice": {
				Token:  "token123",
				Groups: []string{"group1", "group2", "group3"},
			},
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("expected no error for user with unique groups, got: %v", err)
	}
}

func TestValidateConfig_CompleteValid(t *testing.T) {
	config := &Config{
		Server: ServerConfig{
			Host: "localhost:8080",
		},
		Upstream: map[string]UpstreamConfig{
			"calculator": {
				Name:      "calculator",
				Transport: "streamablehttp",
				URL:       "http://localhost:7751/mcp",
			},
			"temperature": {
				Name:      "temperature",
				Transport: "streamablehttp",
				URL:       "http://localhost:7752/mcp",
			},
			"hello": {
				Name:      "hello",
				Transport: "stdio",
				Cmd:       "go",
				CmdArgs:   []string{"run", "main.go"},
			},
		},
		MCPs: map[string]MCPConfig{
			"calc": {
				Name:     "calc",
				Upstream: "calculator",
			},
			"multi": {
				Name: "multi",
				Upstreams: []MultiUpstreamConfig{
					{Name: "calculator", Prefix: "calc"},
					{Name: "temperature", Prefix: "temp"},
					{Name: "hello", Prefix: "hello"},
				},
			},
		},
		Users: map[string]UserConfig{
			"alice": {
				Token:  "alicetoken",
				Groups: []string{"gr1", "gr2"},
			},
			"bob": {
				Token:  "bobtoken",
				Groups: []string{"gr1", "gr3"},
			},
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("expected no error for valid complete config, got: %v", err)
	}
}

func TestValidateConfig_MultipleErrors(t *testing.T) {
	// Test that validation stops at first error
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"invalid1": {
				Name:      "invalid1",
				Transport: "invalid",
			},
			"invalid2": {
				Name:      "invalid2",
				Transport: "stdio",
				// Missing cmd
			},
		},
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
	// Should get at least one error
}

func TestValidateUpstreamConfig_AllTransportTypes(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		url       string
		cmd       string
		args      []string
		wantErr   bool
	}{
		{
			name:      "valid stdio",
			transport: "stdio",
			cmd:       "node",
			args:      []string{"server.js"},
			wantErr:   false,
		},
		{
			name:      "valid sse",
			transport: "streamablehttp",
			url:       "http://localhost:8080/mcp",
			wantErr:   false,
		},
		{
			name:      "valid streamablehttp",
			transport: "streamablehttp",
			url:       "http://localhost:8080/mcp",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Upstream: map[string]UpstreamConfig{
					"test": {
						Name:      "test",
						Transport: tt.transport,
						URL:       tt.url,
						Cmd:       tt.cmd,
						CmdArgs:   tt.args,
					},
				},
				MCPs: map[string]MCPConfig{
					"p1": {
						Name:     "p1",
						Upstream: "test",
					},
				},
			}

			err := config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadConfig_WithValidation(t *testing.T) {
	// Create a temporary config file with invalid content
	tmpFile := t.TempDir() + "/invalid_config.json"
	invalidConfig := `{
		"server": {"host": "localhost:8080"},
		"upstream": {
			"test": {
				"transport": "invalid_transport",
				"url": "http://localhost:8080"
			}
		},
		"mcps": {
			"p1": {"upstream": "test"}
		}
	}`

	if err := writeTestFile(tmpFile, invalidConfig); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := ReadConfig(tmpFile)
	if err == nil {
		t.Fatal("expected ReadConfig to return validation error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid transport") {
		t.Errorf("expected validation error in ReadConfig, got: %v", err)
	}
}

func TestValidateUpstreamConfig_WithBearerToken(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {
				Name:      "test",
				Transport: "streamablehttp",
				URL:       "http://localhost:8080",
				Bearer:    "my-secret-token",
			},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name:     "p1",
				Upstream: "test",
			},
		},
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("expected no error for valid config with bearer token, got: %v", err)
	}
}

func writeTestFile(path, content string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}
