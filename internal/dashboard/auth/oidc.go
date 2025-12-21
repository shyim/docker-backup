package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
)

// Session keys for OIDC
const (
	SessionKeyOIDCUser  = "oidc_user"
	SessionKeyOIDCEmail = "oidc_email"
	SessionKeyOIDCState = "oidc_state"
	SessionKeyOIDCNonce = "oidc_nonce"
)

// OIDCConfig holds OIDC configuration
type OIDCConfig struct {
	Provider       string
	IssuerURL      string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	Scopes         []string
	AllowedUsers   []string
	AllowedDomains []string
}

// OIDCAuth handles OIDC authentication
type OIDCAuth struct {
	provider       *oidc.Provider
	oauth2Config   oauth2.Config
	verifier       *oidc.IDTokenVerifier
	allowedUsers   map[string]bool
	allowedDomains []string
	providerType   string // "google", "github", "oidc"
}

// NewOIDCAuth creates a new OIDC authenticator
func NewOIDCAuth(ctx context.Context, cfg OIDCConfig) (*OIDCAuth, error) {
	auth := &OIDCAuth{
		providerType:   cfg.Provider,
		allowedDomains: cfg.AllowedDomains,
		allowedUsers:   make(map[string]bool),
	}

	// Build allowed users map
	for _, user := range cfg.AllowedUsers {
		auth.allowedUsers[strings.ToLower(user)] = true
	}

	// Set default scopes
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}

	// Configure based on provider type
	var endpoint oauth2.Endpoint
	var issuerURL string

	switch cfg.Provider {
	case "google":
		issuerURL = "https://accounts.google.com"
		endpoint = google.Endpoint
	case "github":
		// GitHub doesn't support standard OIDC, use OAuth2 only
		endpoint = github.Endpoint
		auth.oauth2Config = oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     endpoint,
			Scopes:       []string{"user:email"},
		}
		return auth, nil
	case "oidc":
		if cfg.IssuerURL == "" {
			return nil, fmt.Errorf("issuer-url required for generic OIDC provider")
		}
		issuerURL = cfg.IssuerURL
	default:
		return nil, fmt.Errorf("unknown OIDC provider: %s (supported: google, github, oidc)", cfg.Provider)
	}

	// Initialize OIDC provider (for google and generic)
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}
	auth.provider = provider

	// Create verifier
	auth.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	// Configure OAuth2
	auth.oauth2Config = oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	return auth, nil
}

// ProviderType returns the provider type
func (a *OIDCAuth) ProviderType() string {
	return a.providerType
}

// OAuth2Config returns the OAuth2 configuration
func (a *OIDCAuth) OAuth2Config() *oauth2.Config {
	return &a.oauth2Config
}

// Verifier returns the ID token verifier (nil for GitHub)
func (a *OIDCAuth) Verifier() *oidc.IDTokenVerifier {
	return a.verifier
}

// IsUserAllowed checks if the email is allowed
func (a *OIDCAuth) IsUserAllowed(email string) bool {
	email = strings.ToLower(email)

	// Check explicit user list
	if a.allowedUsers[email] {
		return true
	}

	// Check domain list
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		domain := parts[1]
		for _, allowedDomain := range a.allowedDomains {
			if strings.EqualFold(domain, allowedDomain) {
				return true
			}
		}
	}

	// If no restrictions configured, allow all authenticated users
	return len(a.allowedUsers) == 0 && len(a.allowedDomains) == 0
}

// GenerateState generates a random state for CSRF protection
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// GenerateNonce generates a random nonce for replay protection
func GenerateNonce() (string, error) {
	return GenerateState() // Same implementation
}
