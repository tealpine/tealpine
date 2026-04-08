package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"tealpine/pkg/config"
	"tealpine/pkg/server"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ginCtx creates a minimal gin context backed by a ResponseRecorder.
func ginCtx(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	return c, w
}

var oidcCfg = &config.AuthConfig{
	Type:      "oidc",
	IssuerURL: "https://auth.example.com",
	ClientID:  "test-client",
}

// --- HandleProtectedResourceMetadata ---

func TestHandleProtectedResourceMetadata_OIDCNotConfigured(t *testing.T) {
	h := server.NewMetadataHandlers(nil, "example.com:8080", false)
	c, w := ginCtx("GET", "/.well-known/oauth-protected-resource")
	h.HandleProtectedResourceMetadata(c)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleProtectedResourceMetadata_NonOIDCType(t *testing.T) {
	h := server.NewMetadataHandlers(&config.AuthConfig{Type: "token"}, "example.com:8080", false)
	c, w := ginCtx("GET", "/.well-known/oauth-protected-resource")
	h.HandleProtectedResourceMetadata(c)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleProtectedResourceMetadata_Success_HTTP(t *testing.T) {
	h := server.NewMetadataHandlers(oidcCfg, "example.com:8080", false)
	c, w := ginCtx("GET", "/.well-known/oauth-protected-resource")
	h.HandleProtectedResourceMetadata(c)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "http://example.com:8080", body["resource"])
	require.Equal(t, []interface{}{"https://auth.example.com"}, body["authorization_servers"])
	require.Equal(t, "http://example.com:8080/docs", body["resource_documentation"])
}

func TestHandleProtectedResourceMetadata_Success_HTTPS(t *testing.T) {
	h := server.NewMetadataHandlers(oidcCfg, "example.com:8080", true)
	c, w := ginCtx("GET", "/.well-known/oauth-protected-resource")
	h.HandleProtectedResourceMetadata(c)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "https://example.com:8080", body["resource"])
}

// --- HandlePathSpecificMetadata ---

func TestHandlePathSpecificMetadata_OIDCNotConfigured(t *testing.T) {
	h := server.NewMetadataHandlers(nil, "example.com:8080", false)
	c, w := ginCtx("GET", "/.well-known/oauth-protected-resource/mcp")
	c.Params = gin.Params{{Key: "path", Value: "/mcp"}}
	h.HandlePathSpecificMetadata(c)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandlePathSpecificMetadata_WithPath(t *testing.T) {
	h := server.NewMetadataHandlers(oidcCfg, "example.com:8080", false)
	c, w := ginCtx("GET", "/.well-known/oauth-protected-resource/mcp")
	c.Params = gin.Params{{Key: "path", Value: "/mcp"}}
	h.HandlePathSpecificMetadata(c)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "http://example.com:8080/mcp", body["resource"])
}

func TestHandlePathSpecificMetadata_EmptyPathRedirects(t *testing.T) {
	h := server.NewMetadataHandlers(oidcCfg, "example.com:8080", false)
	c, w := ginCtx("GET", "/.well-known/oauth-protected-resource/")
	c.Params = gin.Params{{Key: "path", Value: ""}}
	h.HandlePathSpecificMetadata(c)
	require.Equal(t, http.StatusFound, w.Code)
	require.Equal(t, "/.well-known/oauth-protected-resource", w.Header().Get("Location"))
}

// --- HandleOpenIDConfiguration ---

func TestHandleOpenIDConfiguration_OIDCNotConfigured(t *testing.T) {
	h := server.NewMetadataHandlers(nil, "example.com:8080", false)
	c, w := ginCtx("GET", "/.well-known/openid-configuration")
	h.HandleOpenIDConfiguration(c)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleOpenIDConfiguration_RedirectsToIssuer(t *testing.T) {
	h := server.NewMetadataHandlers(oidcCfg, "example.com:8080", false)
	c, w := ginCtx("GET", "/.well-known/openid-configuration")
	h.HandleOpenIDConfiguration(c)
	require.Equal(t, http.StatusFound, w.Code)
	require.Equal(t,
		"https://auth.example.com/.well-known/openid-configuration",
		w.Header().Get("Location"),
	)
}

func TestHandleOpenIDConfiguration_IssuerTrailingSlashStripped(t *testing.T) {
	cfg := &config.AuthConfig{Type: "oidc", IssuerURL: "https://auth.example.com/"}
	h := server.NewMetadataHandlers(cfg, "example.com:8080", false)
	c, w := ginCtx("GET", "/.well-known/openid-configuration")
	h.HandleOpenIDConfiguration(c)
	require.Equal(t, http.StatusFound, w.Code)
	require.Equal(t,
		"https://auth.example.com/.well-known/openid-configuration",
		w.Header().Get("Location"),
	)
}

// --- WWWAuthenticateHeader ---

func TestWWWAuthenticateHeader_NoOIDC(t *testing.T) {
	h := server.NewMetadataHandlers(nil, "example.com:8080", false)
	require.Equal(t, `Bearer realm="tealpine"`, h.WWWAuthenticateHeader())
}

func TestWWWAuthenticateHeader_NonOIDCType(t *testing.T) {
	h := server.NewMetadataHandlers(&config.AuthConfig{Type: "token"}, "example.com:8080", false)
	require.Equal(t, `Bearer realm="tealpine"`, h.WWWAuthenticateHeader())
}

func TestWWWAuthenticateHeader_OIDCConfigured(t *testing.T) {
	h := server.NewMetadataHandlers(oidcCfg, "example.com:8080", false)
	header := h.WWWAuthenticateHeader()
	require.Contains(t, header, `as_uri="https://auth.example.com"`)
	require.Contains(t, header, `resource="http://example.com:8080/.well-known/oauth-protected-resource"`)
}
