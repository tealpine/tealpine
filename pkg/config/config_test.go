package config

import (
	"os"
	"strings"
	"testing"
)

func TestValidateUpstreamConfig_InvalidTransport(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {Name: "test", Transport: "invalid", URL: "http://localhost:8080"},
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
			"test": {Name: "test", Transport: "stdio"},
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
			"test": {Name: "test", Transport: "stdio", Cmd: "go", URL: "http://localhost:8080"},
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
			"test": {Name: "test", Transport: "stdio", Cmd: "go", CmdArgs: []string{"run", "main.go"}},
		},
		Groups: map[string][]string{},
		MCPs: map[string]MCPConfig{
			"p1": {Name: "p1", Upstream: "test"},
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
			"test": {Name: "test", Transport: "streamablehttp"},
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
			"test": {Name: "test", Transport: "streamablehttp", URL: "http://localhost:8080", Cmd: "go"},
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
			"test": {Name: "test", Transport: "streamablehttp", URL: "http://localhost:8080", CmdArgs: []string{"arg1"}},
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
			"test": {Name: "test", Transport: "streamablehttp", URL: "http://localhost:8080"},
		},
		Groups: map[string][]string{},
		MCPs: map[string]MCPConfig{
			"p1": {Name: "p1"},
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
			"test1": {Name: "test1", Transport: "streamablehttp", URL: "http://localhost:8080"},
			"test2": {Name: "test2", Transport: "streamablehttp", URL: "http://localhost:8081"},
		},
		Groups: map[string][]string{},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name:      "p1",
				Upstream:  "test1",
				Upstreams: []MultiUpstreamConfig{{Name: "test2", Prefix: "t2"}},
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
			"test": {Name: "test", Transport: "streamablehttp", URL: "http://localhost:8080"},
		},
		Groups: map[string][]string{},
		MCPs: map[string]MCPConfig{
			"p1": {Name: "p1", Upstream: "nonexistent"},
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
			"test1": {Name: "test1", Transport: "streamablehttp", URL: "http://localhost:8080"},
		},
		Groups: map[string][]string{},
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
			"test": {Name: "test", Transport: "streamablehttp", URL: "http://localhost:8080"},
		},
		Groups: map[string][]string{},
		MCPs: map[string]MCPConfig{
			"p1": {Name: "p1", Upstream: "test"},
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
			"test1": {Name: "test1", Transport: "streamablehttp", URL: "http://localhost:8080"},
			"test2": {Name: "test2", Transport: "streamablehttp", URL: "http://localhost:8081"},
		},
		Groups: map[string][]string{},
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

func TestValidateGroupsConfig_UserNotDefined(t *testing.T) {
	config := &Config{
		Groups: map[string][]string{
			"gr1": {"alice", "nonexistent-user"},
		},
		Users: map[string]UserConfig{
			"alice": {Token: "alicetoken"},
		},
	}
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for group referencing undefined user, got nil")
	}
	if !strings.Contains(err.Error(), "user 'nonexistent-user' is not defined in the users section") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateGroupsConfig_AllUsersExist(t *testing.T) {
	config := &Config{
		Groups: map[string][]string{
			"gr1": {"alice", "bob"},
		},
		Users: map[string]UserConfig{
			"alice": {Token: "alicetoken"},
			"bob":   {Token: "bobtoken"},
		},
	}
	err := config.Validate()
	if err != nil {
		t.Errorf("expected no error when all group users exist, got: %v", err)
	}
}

func TestValidateAdminGroups_GroupNotDefined(t *testing.T) {
	config := &Config{
		Server: ServerConfig{
			Admin: []string{"gr-admins"},
		},
		Groups: map[string][]string{},
	}
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for admin referencing undefined group, got nil")
	}
	if !strings.Contains(err.Error(), "server.admin: group 'gr-admins' is not defined in the groups section") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateAdminGroups_GroupDefined(t *testing.T) {
	config := &Config{
		Server: ServerConfig{
			Host:  "localhost:8080",
			Admin: []string{"gr-admins"},
		},
		Groups: map[string][]string{
			"gr-admins": {"alice"},
		},
		Users: map[string]UserConfig{
			"alice": {Token: "alicetoken"},
		},
	}
	err := config.Validate()
	if err != nil {
		t.Errorf("expected no error when admin group is defined, got: %v", err)
	}
}

func TestValidateMCPAuthGroups_GroupNotDefined(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {Name: "test", Transport: "streamablehttp", URL: "http://localhost:8080"},
		},
		Groups: map[string][]string{},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name:     "p1",
				Upstream: "test",
				Auth: map[string][]AuthRule{
					"undefined-group": {{Method: "tools/list", Allow: []string{"*"}}},
				},
			},
		},
	}
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for mcp auth referencing undefined group, got nil")
	}
	if !strings.Contains(err.Error(), "auth group 'undefined-group' is not defined in the groups section") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateMCPAuthGroups_GroupDefined(t *testing.T) {
	config := &Config{
		Upstream: map[string]UpstreamConfig{
			"test": {Name: "test", Transport: "streamablehttp", URL: "http://localhost:8080"},
		},
		Groups: map[string][]string{
			"gr1": {"alice"},
		},
		Users: map[string]UserConfig{
			"alice": {Token: "alicetoken"},
		},
		MCPs: map[string]MCPConfig{
			"p1": {
				Name:     "p1",
				Upstream: "test",
				Auth: map[string][]AuthRule{
					"gr1": {{Method: "tools/list", Allow: []string{"*"}}},
				},
			},
		},
	}
	err := config.Validate()
	if err != nil {
		t.Errorf("expected no error when mcp auth group is defined, got: %v", err)
	}
}

func TestValidateConfig_CompleteValid(t *testing.T) {
	config := &Config{
		Server: ServerConfig{
			Host:  "localhost:8080",
			Admin: []string{"admins"},
		},
		Upstream: map[string]UpstreamConfig{
			"calculator":  {Name: "calculator", Transport: "streamablehttp", URL: "http://localhost:7751/mcp"},
			"temperature": {Name: "temperature", Transport: "streamablehttp", URL: "http://localhost:7752/mcp"},
			"hello":       {Name: "hello", Transport: "stdio", Cmd: "go", CmdArgs: []string{"run", "main.go"}},
		},
		Groups: map[string][]string{
			"admins": {"alice"},
			"users":  {"alice", "bob"},
		},
		Users: map[string]UserConfig{
			"alice": {Token: "alicetoken"},
			"bob":   {Token: "bobtoken"},
		},
		MCPs: map[string]MCPConfig{
			"calc": {
				Name:     "calc",
				Upstream: "calculator",
				Auth: map[string][]AuthRule{
					"users": {{Method: "tools/call", Allow: []string{"*"}}},
				},
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
	}
	err := config.Validate()
	if err != nil {
		t.Errorf("expected no error for valid complete config, got: %v", err)
	}
}

func TestReadConfig_WithValidation(t *testing.T) {
	tmpFile := t.TempDir() + "/invalid_config.json"
	invalidConfig := `{
		"server": {"host": "localhost:8080"},
		"upstream": {
			"test": {
				"transport": "invalid_transport",
				"url": "http://localhost:8080"
			}
		},
		"groups": {},
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
			"test": {Name: "test", Transport: "streamablehttp", URL: "http://localhost:8080", Bearer: "my-secret-token"},
		},
		Groups: map[string][]string{},
		MCPs: map[string]MCPConfig{
			"p1": {Name: "p1", Upstream: "test"},
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
