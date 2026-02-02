package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// UpstreamAuthInfo holds discovered auth info for an upstream MCP server
type UpstreamAuthInfo struct {
	AuthServerURL         string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	ClientID              string
}

// OIDCEndpoints holds the discovered OIDC endpoints
type OIDCEndpoints struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// DiscoverAuthServer performs RFC 9728 Resource Metadata Discovery.
// It fetches /.well-known/oauth-protected-resource from the MCP server
// and returns the first authorization_servers entry.
func DiscoverAuthServer(mcpURL string) (string, error) {
	parsed, err := url.Parse(mcpURL)
	if err != nil {
		return "", fmt.Errorf("invalid MCP URL: %w", err)
	}

	wellKnownURL := fmt.Sprintf("%s://%s/.well-known/oauth-protected-resource", parsed.Scheme, parsed.Host)

	resp, err := httpClient.Get(wellKnownURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch resource metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resource metadata endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read resource metadata: %w", err)
	}

	var metadata struct {
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil {
		return "", fmt.Errorf("failed to parse resource metadata: %w", err)
	}

	if len(metadata.AuthorizationServers) == 0 {
		return "", fmt.Errorf("no authorization_servers in resource metadata")
	}

	return metadata.AuthorizationServers[0], nil
}

// DiscoverOIDCEndpoints fetches the authorization server metadata.
// Tries RFC 8414 (/.well-known/oauth-authorization-server) first,
// then falls back to OpenID Connect discovery (/.well-known/openid-configuration).
func DiscoverOIDCEndpoints(authServerURL string) (*OIDCEndpoints, error) {
	parsed, err := url.Parse(authServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid auth server URL: %w", err)
	}

	// RFC 8414: well-known is at the origin (scheme + host), not under the path
	origin := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
	urls := []string{
		origin + "/.well-known/oauth-authorization-server",
		origin + "/.well-known/openid-configuration",
	}

	for _, wellKnownURL := range urls {
		endpoints, err := fetchEndpoints(wellKnownURL)
		if err == nil {
			return endpoints, nil
		}
	}

	return nil, fmt.Errorf("no authorization server metadata found at %s (tried oauth-authorization-server and openid-configuration)", origin)
}

func fetchEndpoints(wellKnownURL string) (*OIDCEndpoints, error) {
	resp, err := httpClient.Get(wellKnownURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("%s returned %d", wellKnownURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var endpoints OIDCEndpoints
	if err := json.Unmarshal(body, &endpoints); err != nil {
		return nil, err
	}

	if endpoints.AuthorizationEndpoint == "" {
		return nil, fmt.Errorf("missing authorization_endpoint")
	}
	if endpoints.TokenEndpoint == "" {
		return nil, fmt.Errorf("missing token_endpoint")
	}

	return &endpoints, nil
}

// RegisterClient performs RFC 7591 Dynamic Client Registration.
// It registers tealpine as a public OAuth2 client using PKCE.
func RegisterClient(registrationEndpoint, redirectURL string) (string, error) {
	reqBody := map[string]interface{}{
		"client_name":                "tealpine",
		"redirect_uris":             []string{redirectURL},
		"grant_types":               []string{"authorization_code"},
		"response_types":            []string{"code"},
		"token_endpoint_auth_method": "none",
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal registration request: %w", err)
	}

	resp, err := httpClient.Post(registrationEndpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to register client: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("client registration returned %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read registration response: %w", err)
	}

	var result struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse registration response: %w", err)
	}

	if result.ClientID == "" {
		return "", fmt.Errorf("no client_id in registration response")
	}

	return result.ClientID, nil
}

// DiscoverAndRegister performs the full upstream auth discovery flow:
// 1. RFC 9728 resource metadata discovery
// 2. OIDC discovery
// 3. Dynamic client registration (if no clientID override)
// It returns the complete UpstreamAuthInfo.
func DiscoverAndRegister(mcpURL, redirectURL, clientIDOverride string) (*UpstreamAuthInfo, error) {
	// Step 1: Discover auth server via RFC 9728
	authServerURL, err := DiscoverAuthServer(mcpURL)
	if err != nil {
		return nil, fmt.Errorf("auth server discovery failed: %w", err)
	}

	// Step 2: Discover OIDC endpoints
	endpoints, err := DiscoverOIDCEndpoints(authServerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed: %w", err)
	}

	info := &UpstreamAuthInfo{
		AuthServerURL:         authServerURL,
		AuthorizationEndpoint: endpoints.AuthorizationEndpoint,
		TokenEndpoint:         endpoints.TokenEndpoint,
		RegistrationEndpoint:  endpoints.RegistrationEndpoint,
	}

	// Step 3: Use override or register dynamically
	if clientIDOverride != "" {
		info.ClientID = clientIDOverride
	} else if redirectURL != "" && endpoints.RegistrationEndpoint != "" {
		// Only register when we have a real redirect URL
		clientID, err := RegisterClient(endpoints.RegistrationEndpoint, redirectURL)
		if err != nil {
			return nil, fmt.Errorf("dynamic client registration failed: %w", err)
		}
		info.ClientID = clientID
	}
	// ClientID may be empty here — registration will be deferred to the login handler

	return info, nil
}

// ProbeUpstreamAuth sends a GET request to the MCP server URL to check
// if it requires authentication. Returns true if the server responds with 401.
func ProbeUpstreamAuth(mcpURL string) (bool, error) {
	resp, err := httpClient.Get(mcpURL)
	if err != nil {
		return false, fmt.Errorf("failed to probe upstream server: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	return resp.StatusCode == http.StatusUnauthorized, nil
}
