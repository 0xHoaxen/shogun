package main

import (
	"fmt"

	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/services/katana/internal/github"
)

const (
	githubClientIDEnv     = "KATANA_GITHUB_CLIENT_ID"
	githubClientSecretEnv = "KATANA_GITHUB_CLIENT_SECRET"
	githubRedirectURLEnv  = "KATANA_GITHUB_REDIRECT_URL"

	// Test only: point the OAuth flow at a stub instead of github.com.
	githubOAuthBaseEnv = "KATANA_GITHUB_OAUTH_BASE"
)

// newGitHubOAuth builds the GitHub OAuth app users connect through. It is the
// app's own identity (not a user's token). With none of the three settings the
// connection is off, which is allowed outside production; a partial set is
// always a mistake, and production requires all three.
func newGitHubOAuth(lookup config.LookupFunc, production bool) (*github.OAuth, error) {
	var cfg github.OAuthConfig
	settings := []struct {
		env string
		dst *string
	}{
		{githubClientIDEnv, &cfg.ClientID},
		{githubClientSecretEnv, &cfg.ClientSecret},
		{githubRedirectURLEnv, &cfg.RedirectURL},
	}
	set := 0
	for _, s := range settings {
		*s.dst = config.String(lookup, s.env, "")
		if *s.dst != "" {
			set++
		}
	}
	if set == 0 && !production {
		return nil, nil
	}
	for _, s := range settings {
		if *s.dst == "" {
			return nil, fmt.Errorf("config: %s is required (set all of %s, %s and %s, or none to turn GitHub connecting off)",
				s.env, githubClientIDEnv, githubClientSecretEnv, githubRedirectURLEnv)
		}
	}
	if base := config.String(lookup, githubOAuthBaseEnv, ""); base != "" {
		cfg.AuthorizeBase, cfg.TokenBase = base, base
	}
	if api := config.String(lookup, githubAPIBaseEnv, ""); api != "" {
		cfg.APIBase = api
	}
	return github.NewOAuth(cfg, nil), nil
}
