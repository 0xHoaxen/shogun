// Package httpauth serves torii's browser login: the Google OAuth redirect
// flow under /auth/*, and the session cookie helpers the API layer shares.
package httpauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/0xHoaxen/shogun/services/torii/internal/app"
)

// Provider is the identity provider side of the login flow.
type Provider interface {
	// AuthURL returns the provider page to send the browser to. verifier is
	// the PKCE code verifier; only its S256 challenge goes into the URL.
	AuthURL(state, verifier string) string
	// Exchange trades the authorization code for verified identity claims.
	Exchange(ctx context.Context, code, verifier string) (app.Claims, error)
}

// ProviderConfig configures an OIDC provider.
type ProviderConfig struct {
	// IssuerURL is the OIDC issuer; Google's is https://accounts.google.com.
	IssuerURL    string
	ClientID     string
	ClientSecret string
	// RedirectURL is the absolute URL of /auth/callback.
	RedirectURL string
}

// OIDCProvider implements Provider for any OpenID Connect issuer.
type OIDCProvider struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewOIDCProvider discovers the issuer's endpoints and keys.
func NewOIDCProvider(ctx context.Context, cfg ProviderConfig) (*OIDCProvider, error) {
	discovered, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover oidc issuer: %w", err)
	}
	return &OIDCProvider{
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     discovered.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: discovered.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

// AuthURL implements Provider.
func (p *OIDCProvider) AuthURL(state, verifier string) string {
	return p.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

// Exchange implements Provider.
func (p *OIDCProvider) Exchange(ctx context.Context, code, verifier string) (app.Claims, error) {
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return app.Claims{}, fmt.Errorf("exchange code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return app.Claims{}, errors.New("token response has no id_token")
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return app.Claims{}, fmt.Errorf("verify id_token: %w", err)
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return app.Claims{}, fmt.Errorf("decode id_token claims: %w", err)
	}
	return app.Claims{
		Subject:       idToken.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		DisplayName:   claims.Name,
	}, nil
}
