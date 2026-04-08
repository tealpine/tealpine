package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"tealpine/pkg/config"
)

// OIDCProvider wraps the OIDC provider and OAuth2 configuration
type OIDCProvider struct {
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config oauth2.Config
}

// NewOIDCProvider creates a new OIDC provider from configuration
func NewOIDCProvider(cfg *config.AuthConfig, redirectURL string) (*OIDCProvider, error) {
	// Use timeout context for provider initialization
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize OIDC provider from issuer URL
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	// Create JWT verifier with strict validation (no skips)
	verifier := provider.Verifier(&oidc.Config{
		ClientID:          cfg.ClientID,
		SkipClientIDCheck: false, // Always verify audience
		SkipExpiryCheck:   false, // Always verify expiry
		SkipIssuerCheck:   false, // Always verify issuer
	})

	// Set up OAuth2 config with authorization/token endpoints
	oauth2Config := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	return &OIDCProvider{
		provider:     provider,
		verifier:     verifier,
		oauth2Config: oauth2Config,
	}, nil
}

// ValidateJWT validates a JWT token and returns the ID token if valid
func (p *OIDCProvider) ValidateJWT(ctx context.Context, token string) (*oidc.IDToken, error) {
	// Parse and verify JWT signature using provider's keys
	// Validates issuer, audience (clientID), expiry, not-before
	idToken, err := p.verifier.Verify(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("failed to verify JWT token: %w", err)
	}

	return idToken, nil
}

// ExtractClaims extracts all claims from an ID token
func (p *OIDCProvider) ExtractClaims(idToken *oidc.IDToken) (map[string]interface{}, error) {
	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to extract claims: %w", err)
	}

	return claims, nil
}

// GenerateAuthURL creates an authorization URL with PKCE
func (p *OIDCProvider) GenerateAuthURL(state, codeVerifier string) string {
	// Compute S256 challenge: base64url(SHA256(verifier))
	challenge := computePKCEChallenge(codeVerifier)

	// Build authorization URL with PKCE
	return p.oauth2Config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// ExchangeCode exchanges an authorization code for tokens with PKCE verifier
func (p *OIDCProvider) ExchangeCode(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error) {
	// Exchange code for token with PKCE verifier
	token, err := p.oauth2Config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	return token, nil
}

// computePKCEChallenge computes the S256 PKCE challenge from a code verifier
func computePKCEChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// IntrospectToken validates an access token using the userinfo endpoint
// This is used for opaque access tokens that aren't JWTs
func (p *OIDCProvider) IntrospectToken(ctx context.Context, token string) (map[string]interface{}, error) {
	// Call userinfo endpoint with access token to validate it
	// This validates the token and returns user information
	userinfo, err := p.provider.UserInfo(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token,
		TokenType:   "Bearer",
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to validate access token via userinfo: %w", err)
	}

	// Extract claims from userinfo
	var claims map[string]interface{}
	if err := userinfo.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to extract userinfo claims: %w", err)
	}

	return claims, nil
}
