package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

const (
	defaultAuthorizeBase = "https://github.com"
	authorizePath        = "/login/oauth/authorize"
	tokenPath            = "/login/oauth/access_token"
	userPath             = "/user"
)

// OAuthConfig is the GitHub OAuth app Shogun is registered as. It is the app's
// own identity, not a user's: each user's token comes from their own consent.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the callback registered with the app.
	RedirectURL string

	// AuthorizeBase, TokenBase and APIBase point at GitHub's three hosts. Empty
	// means github.com and api.github.com; tests point them at a fake.
	AuthorizeBase string
	TokenBase     string
	APIBase       string
}

// Configured reports whether the app is set up well enough to connect users.
func (c OAuthConfig) Configured() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.RedirectURL != ""
}

// OAuth runs GitHub's authorization-code flow. It requests no scope, so a token
// can read only public data and can never change anything on GitHub.
type OAuth struct {
	cfg  OAuthConfig
	http *http.Client
}

// NewOAuth returns an OAuth for the app cfg. A nil httpClient means a client with
// a timeout.
func NewOAuth(cfg OAuthConfig, httpClient *http.Client) *OAuth {
	if cfg.AuthorizeBase == "" {
		cfg.AuthorizeBase = defaultAuthorizeBase
	}
	if cfg.TokenBase == "" {
		cfg.TokenBase = defaultAuthorizeBase
	}
	if cfg.APIBase == "" {
		cfg.APIBase = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	cfg.AuthorizeBase = strings.TrimRight(cfg.AuthorizeBase, "/")
	cfg.TokenBase = strings.TrimRight(cfg.TokenBase, "/")
	cfg.APIBase = strings.TrimRight(cfg.APIBase, "/")
	return &OAuth{cfg: cfg, http: httpClient}
}

// AuthURL is where to send the user to grant access. state comes back unchanged
// to the callback.
func (o *OAuth) AuthURL(state string) string {
	q := url.Values{
		"client_id":    {o.cfg.ClientID},
		"redirect_uri": {o.cfg.RedirectURL},
		"state":        {state},
	}
	return o.cfg.AuthorizeBase + authorizePath + "?" + q.Encode()
}

type tokenJSON struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

// Exchange trades the authorization code for the user's access token. A code
// GitHub refuses is domain.ErrCodeRefused; anything else is domain.ErrUnavailable.
// No error repeats the code, the app's secret or a response body.
func (o *OAuth) Exchange(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"client_id":     {o.cfg.ClientID},
		"client_secret": {o.cfg.ClientSecret},
		"code":          {code},
		"redirect_uri":  {o.cfg.RedirectURL},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.TokenBase+tokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("github oauth: build request: %w", domain.ErrUnavailable)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "shogun-katana")
	resp, err := o.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("github oauth: request failed: %w", domain.ErrUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github oauth: token endpoint answered %d: %w", resp.StatusCode, domain.ErrUnavailable)
	}
	var out tokenJSON
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&out); err != nil {
		return "", fmt.Errorf("github oauth: decode token response: %w", domain.ErrUnavailable)
	}
	switch {
	case out.Error == "bad_verification_code":
		// GitHub answers 200 for a code that is wrong, expired or already used.
		return "", domain.ErrCodeRefused
	case out.Error != "":
		return "", fmt.Errorf("github oauth: token endpoint refused the app (%s): %w", out.Error, domain.ErrUnavailable)
	case out.AccessToken == "":
		return "", fmt.Errorf("github oauth: no token in the answer: %w", domain.ErrUnavailable)
	}
	return out.AccessToken, nil
}

// Login reads the login the token belongs to, so the connection can show which
// GitHub account is linked and so syncs know whose repositories to read.
func (o *OAuth) Login(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.cfg.APIBase+userPath, nil)
	if err != nil {
		return "", fmt.Errorf("github oauth: build request: %w", domain.ErrUnavailable)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "shogun-katana")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := o.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("github oauth: request failed: %w", domain.ErrUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", domain.ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github oauth: user endpoint answered %d: %w", resp.StatusCode, domain.ErrUnavailable)
	}
	var out struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&out); err != nil {
		return "", fmt.Errorf("github oauth: decode user: %w", domain.ErrUnavailable)
	}
	login := strings.TrimSpace(out.Login)
	if login == "" {
		return "", fmt.Errorf("github oauth: no login in the answer: %w", domain.ErrUnavailable)
	}
	return login, nil
}
