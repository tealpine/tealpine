package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"tealpine/pkg/config"
)

// MetadataHandlers handles OAuth 2.0 Protected Resource Metadata endpoints (RFC 9728)
type MetadataHandlers struct {
	authConfig *config.AuthConfig
	serverHost string
}

// NewMetadataHandlers creates a new MetadataHandlers instance
func NewMetadataHandlers(authConfig *config.AuthConfig, serverHost string) *MetadataHandlers {
	return &MetadataHandlers{
		authConfig: authConfig,
		serverHost: serverHost,
	}
}

// resourceServerURL returns the full resource server URL with the appropriate scheme
func (h *MetadataHandlers) resourceServerURL() string {
	scheme := "http"
	if strings.HasPrefix(h.authConfig.RedirectURL, "https://") {
		scheme = "https"
	}
	return scheme + "://" + h.serverHost
}

// buildProtectedResourceMetadata constructs the OAuth 2.0 Protected Resource Metadata.
// If path is non-empty, it's appended to the resource identifier.
func (h *MetadataHandlers) buildProtectedResourceMetadata(path string) map[string]interface{} {
	resourceServer := h.resourceServerURL()

	resource := resourceServer
	if path != "" {
		resource = resourceServer + "/" + strings.TrimPrefix(path, "/")
	}

	return map[string]interface{}{
		"resource":                 resource,
		"authorization_servers":    []string{h.authConfig.IssuerURL},
		"scopes_supported":         []string{"openid", "profile", "email"},
		"bearer_methods_supported": []string{"header"},
		"resource_documentation":   resourceServer + "/docs",
	}
}

// HandleProtectedResourceMetadata serves the main protected resource metadata
// Path: /.well-known/oauth-protected-resource
func (h *MetadataHandlers) HandleProtectedResourceMetadata(c *gin.Context) {
	if h.authConfig == nil || h.authConfig.Type != "oidc" {
		c.JSON(http.StatusNotFound, gin.H{"error": "OAuth not configured"})
		return
	}
	c.JSON(http.StatusOK, h.buildProtectedResourceMetadata(""))
}

// HandlePathSpecificMetadata serves path-specific protected resource metadata
// Path: /.well-known/oauth-protected-resource/*path
func (h *MetadataHandlers) HandlePathSpecificMetadata(c *gin.Context) {
	if h.authConfig == nil || h.authConfig.Type != "oidc" {
		c.JSON(http.StatusNotFound, gin.H{"error": "OAuth not configured"})
		return
	}

	path := c.Param("path")
	if path == "" {
		c.Redirect(http.StatusFound, "/.well-known/oauth-protected-resource")
		return
	}
	c.JSON(http.StatusOK, h.buildProtectedResourceMetadata(path))
}

// HandleOpenIDConfiguration serves OpenID Connect Discovery metadata
// Path: /.well-known/openid-configuration
// Note: This should typically be served by the authorization server (OIDC provider),
// but we can proxy/redirect to the issuer's discovery endpoint
func (h *MetadataHandlers) HandleOpenIDConfiguration(c *gin.Context) {
	if h.authConfig == nil || h.authConfig.Type != "oidc" {
		c.JSON(http.StatusNotFound, gin.H{"error": "OIDC not configured"})
		return
	}

	// Redirect to the OIDC provider's discovery endpoint
	discoveryURL := strings.TrimSuffix(h.authConfig.IssuerURL, "/") + "/.well-known/openid-configuration"
	c.Redirect(http.StatusFound, discoveryURL)
}

// WWWAuthenticateHeader returns the WWW-Authenticate header value pointing to metadata.
// This is called when authentication fails and helps clients discover how to authenticate.
func (h *MetadataHandlers) WWWAuthenticateHeader() string {
	if h.authConfig == nil || h.authConfig.Type != "oidc" {
		return `Bearer realm="tealpine"`
	}

	resourceServer := h.resourceServerURL()
	return `Bearer realm="tealpine", as_uri="` + h.authConfig.IssuerURL + `", resource="` + resourceServer + `/.well-known/oauth-protected-resource"`
}
