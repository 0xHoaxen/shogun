package main

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/pkg/server"
	"github.com/0xHoaxen/shogun/services/torii/internal/idp/idptest"
)

const (
	testClientID     = "test-client"
	testClientSecret = "test-client-secret"
	testOwnerEmail   = "owner@example.com"
	testOversizeCap  = "1024"
	oversizeBody     = 4096
)

// addLoginSettings adds the settings torii requires to env, backed by a fake
// identity provider that the test can issue login codes from.
func addLoginSettings(t *testing.T, env map[string]string) *idptest.IDP {
	t.Helper()
	idp := idptest.New(t, testClientID, testClientSecret)
	env["GOOGLE_CLIENT_ID"] = testClientID
	env["GOOGLE_CLIENT_SECRET"] = testClientSecret
	env["GOOGLE_ISSUER_URL"] = idp.IssuerURL()
	env["TORII_ALLOWED_EMAILS"] = testOwnerEmail
	env["TORII_PUBLIC_ADDR"] = "127.0.0.1:0"
	return idp
}

// startTorii runs torii against a fresh database and returns the base URL of
// its public API. The service stops when the test ends.
func startTorii(t *testing.T, extraEnv map[string]string) (string, *idptest.IDP) {
	t.Helper()
	env := map[string]string{
		"ENVIRONMENT":          "test",
		"DATABASE_URL":         postgrestest.NewDatabase(t),
		"IDENTITY_SIGNING_KEY": testIdentityKey,
	}
	idp := addLoginSettings(t, env)
	for k, v := range extraEnv {
		env[k] = v
	}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	grpcLis, httpLis, publicLis := listen(t), listen(t), listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, lookup, runOverrides{publicListener: publicLis}, server.WithListeners(grpcLis, httpLis))
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(exitTimeout):
			t.Error("run did not exit after cancel")
		}
	})
	checkHealth(t, grpcLis.Addr().String())
	return "http://" + publicLis.Addr().String(), idp
}

func newBrowser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// reply is what a test needs of a response, with its body already closed.
type reply struct {
	StatusCode int
	Header     http.Header
}

func get(t *testing.T, client *http.Client, target string) reply {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return reply{StatusCode: resp.StatusCode, Header: resp.Header}
}

func TestRunServesLoginAndSession(t *testing.T) {
	// Arrange
	base, idp := startTorii(t, nil)
	browser := newBrowser(t)
	api := apiv1connect.NewAuthServiceClient(browser, base)

	// Act: the browser starts a login, the provider approves, the browser returns.
	login := get(t, browser, base+"/auth/login")
	providerURL, err := url.Parse(login.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse provider redirect: %v", err)
	}
	code := idp.IssueCode(providerURL.Query().Get("code_challenge"),
		idptest.Claims{Subject: "sub-1", Email: testOwnerEmail, EmailVerified: true, Name: "Owner"})
	callback := get(t, browser, base+"/auth/callback?"+url.Values{
		"code": {code}, "state": {providerURL.Query().Get("state")},
	}.Encode())
	session, sessionErr := api.GetSession(context.Background(), connect.NewRequest(&apiv1.GetSessionRequest{}))

	// Assert
	if callback.StatusCode != http.StatusFound || callback.Header.Get("Location") != postLoginPath {
		t.Fatalf("callback = %d %q, want 302 %q", callback.StatusCode, callback.Header.Get("Location"), postLoginPath)
	}
	if sessionErr != nil {
		t.Fatalf("GetSession after login: %v", sessionErr)
	}
	if got := session.Msg.GetSession().GetEmail(); got != testOwnerEmail {
		t.Fatalf("session email = %q, want %q", got, testOwnerEmail)
	}
}

func TestRunAPIWithoutSessionIsUnauthenticated(t *testing.T) {
	// Arrange
	base, _ := startTorii(t, nil)
	api := apiv1connect.NewAuthServiceClient(newBrowser(t), base)

	// Act
	_, err := api.GetSession(context.Background(), connect.NewRequest(&apiv1.GetSessionRequest{}))

	// Assert
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("code = %v, want Unauthenticated (err %v)", connect.CodeOf(err), err)
	}
}

func TestRunRejectsOversizeRequestBodies(t *testing.T) {
	// Arrange
	base, _ := startTorii(t, map[string]string{"TORII_MAX_BODY_BYTES": testOversizeCap})
	body := `{"pad":"` + strings.Repeat("a", oversizeBody) + `"}`

	// Act
	resp, err := http.Post(base+apiv1connect.AuthServiceLogoutProcedure, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	reply, _ := io.ReadAll(resp.Body)

	// Assert
	if resp.StatusCode == http.StatusOK || !strings.Contains(string(reply), "resource_exhausted") {
		t.Fatalf("oversize body got %d %s, want a resource_exhausted error", resp.StatusCode, reply)
	}
}

func TestRunRejectsIncompleteLoginSettings(t *testing.T) {
	// Arrange
	env := map[string]string{
		"ENVIRONMENT":          "test",
		"DATABASE_URL":         "postgres://unused",
		"IDENTITY_SIGNING_KEY": testIdentityKey,
	}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	// Act
	err := run(context.Background(), lookup, runOverrides{})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "GOOGLE_CLIENT_ID") {
		t.Fatalf("err = %v, want mention of GOOGLE_CLIENT_ID", err)
	}
}
