package github_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/github"
)

const (
	clientSecret = "oauth-client-secret-9876"
	grantCode    = "code-from-github-123"
)

func oauthApp(base string) github.OAuthConfig {
	return github.OAuthConfig{
		ClientID: "Iv1.abc", ClientSecret: clientSecret, RedirectURL: "https://shogun.example/settings/github/callback",
		AuthorizeBase: base, TokenBase: base, APIBase: base,
	}
}

func TestAuthURLSendsTheOwnerToGitHubWithOurStateAndNoScope(t *testing.T) {
	oauth := github.NewOAuth(oauthApp("https://github.com"), nil)

	raw := oauth.AuthURL("sealed-state")

	u, err := url.Parse(raw)
	if err != nil || u.Host != "github.com" || u.Path != "/login/oauth/authorize" {
		t.Fatalf("url %q, err %v", raw, err)
	}
	q := u.Query()
	if q.Get("client_id") != "Iv1.abc" || q.Get("state") != "sealed-state" ||
		q.Get("redirect_uri") != "https://shogun.example/settings/github/callback" {
		t.Fatalf("query = %v", q)
	}
	if q.Has("scope") {
		t.Fatalf("a scope was requested: %q. Public data needs none, and asking for more would give the app write access it never uses", q.Get("scope"))
	}
	if strings.Contains(raw, clientSecret) {
		t.Fatal("the client secret is in the authorize URL")
	}
}

// fakeGitHubOAuth answers the token and user endpoints.
type fakeGitHubOAuth struct {
	mu        sync.Mutex
	tokenForm url.Values
	userAuth  string
	tokenBody string
	tokenCode int
	userBody  string
	userCode  int
}

func (f *fakeGitHubOAuth) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/oauth/access_token":
			raw, _ := io.ReadAll(r.Body)
			f.tokenForm, _ = url.ParseQuery(string(raw))
			w.WriteHeader(f.tokenCode)
			_, _ = io.WriteString(w, f.tokenBody)
		case "/user":
			f.userAuth = r.Header.Get("Authorization")
			w.WriteHeader(f.userCode)
			_, _ = io.WriteString(w, f.userBody)
		default:
			http.NotFound(w, r)
		}
	})
}

func newFake(t *testing.T) (*fakeGitHubOAuth, github.OAuthConfig) {
	t.Helper()
	f := &fakeGitHubOAuth{
		tokenCode: http.StatusOK, tokenBody: `{"access_token":"gho_granted-token-1","token_type":"bearer","scope":""}`,
		userCode: http.StatusOK, userBody: `{"login":"octocat"}`,
	}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return f, oauthApp(srv.URL)
}

func TestExchangeTradesTheCodeForATokenUsingTheAppsOwnSecret(t *testing.T) {
	f, cfg := newFake(t)

	token, err := github.NewOAuth(cfg, nil).Exchange(context.Background(), grantCode)

	if err != nil || token != "gho_granted-token-1" {
		t.Fatalf("token %q, err %v", token, err)
	}
	if f.tokenForm.Get("code") != grantCode || f.tokenForm.Get("client_secret") != clientSecret || f.tokenForm.Get("client_id") != "Iv1.abc" ||
		f.tokenForm.Get("redirect_uri") != cfg.RedirectURL {
		t.Fatalf("GitHub received %v", f.tokenForm)
	}
}

func TestExchangeSaysTheCodeWasRefusedWithoutRepeatingAnything(t *testing.T) {
	// GitHub answers 200 with an error in the body for a bad or reused code.
	f, cfg := newFake(t)
	f.tokenBody = `{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired. code=` + grantCode + `"}`

	_, err := github.NewOAuth(cfg, nil).Exchange(context.Background(), grantCode)

	if !errors.Is(err, domain.ErrCodeRefused) {
		t.Fatalf("err = %v, want ErrCodeRefused", err)
	}
	for _, secret := range []string{grantCode, clientSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("the error repeats %q: %v", secret, err)
		}
	}
}

func TestExchangeFailuresAreUnavailableNotRefused(t *testing.T) {
	for name, tt := range map[string]struct {
		code int
		body string
	}{
		"server error":  {http.StatusInternalServerError, `oops`},
		"not json":      {http.StatusOK, `<html>`},
		"no token":      {http.StatusOK, `{"token_type":"bearer"}`},
		"other error":   {http.StatusOK, `{"error":"incorrect_client_credentials"}`},
		"rate limited":  {http.StatusTooManyRequests, `{}`},
		"empty success": {http.StatusOK, `{}`},
	} {
		t.Run(name, func(t *testing.T) {
			f, cfg := newFake(t)
			f.tokenCode, f.tokenBody = tt.code, tt.body

			_, err := github.NewOAuth(cfg, nil).Exchange(context.Background(), grantCode)

			if err == nil || errors.Is(err, domain.ErrCodeRefused) {
				t.Fatalf("err = %v, want a failure that is not a refused code", err)
			}
			if strings.Contains(err.Error(), clientSecret) || strings.Contains(err.Error(), grantCode) {
				t.Fatalf("the error repeats a secret: %v", err)
			}
		})
	}
}

func TestLoginReadsWhoTheTokenBelongsTo(t *testing.T) {
	f, cfg := newFake(t)

	login, err := github.NewOAuth(cfg, nil).Login(context.Background(), "gho_granted-token-1")

	if err != nil || login != "octocat" {
		t.Fatalf("login %q, err %v", login, err)
	}
	if f.userAuth != "Bearer gho_granted-token-1" {
		t.Fatalf("Authorization = %q", f.userAuth)
	}
}

func TestLoginFailsClosedOnAnythingUnusable(t *testing.T) {
	for name, tt := range map[string]struct {
		code int
		body string
	}{
		"rejected token": {http.StatusUnauthorized, `{}`},
		"no login":       {http.StatusOK, `{}`},
		"blank login":    {http.StatusOK, `{"login":"  "}`},
		"not json":       {http.StatusOK, `nope`},
	} {
		t.Run(name, func(t *testing.T) {
			f, cfg := newFake(t)
			f.userCode, f.userBody = tt.code, tt.body

			login, err := github.NewOAuth(cfg, nil).Login(context.Background(), "gho_granted-token-1")

			if err == nil || login != "" {
				t.Fatalf("login %q, err %v; want a failure and no login", login, err)
			}
			if strings.Contains(err.Error(), "gho_granted-token-1") {
				t.Fatalf("the error repeats the token: %v", err)
			}
		})
	}
}

func TestOAuthIsOnlyConfiguredWhenAllThreeSettingsAre(t *testing.T) {
	for name, cfg := range map[string]github.OAuthConfig{
		"all":         {ClientID: "a", ClientSecret: "b", RedirectURL: "https://x/y"},
		"no id":       {ClientSecret: "b", RedirectURL: "https://x/y"},
		"no secret":   {ClientID: "a", RedirectURL: "https://x/y"},
		"no redirect": {ClientID: "a", ClientSecret: "b"},
	} {
		if got, want := cfg.Configured(), name == "all"; got != want {
			t.Errorf("%s: Configured() = %v, want %v", name, got, want)
		}
	}
}
