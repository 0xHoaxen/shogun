package main

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
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
	// The connection is lazy, so tests that never call kagami need no server.
	env["KAGAMI_ADDR"] = "127.0.0.1:1"
	env["SOROBAN_ADDR"] = "127.0.0.1:1"
	env["FUDE_ADDR"] = "127.0.0.1:1"
	env["TSUBAME_ADDR"] = "127.0.0.1:1"
	env["TAIKO_ADDR"] = "127.0.0.1:1"
	env["DOJO_ADDR"] = "127.0.0.1:1"
	env["KATANA_ADDR"] = "127.0.0.1:1"
	return idp
}

// signIn walks the browser through the login: torii sends it to the provider,
// the provider approves the owner, and the browser returns to the callback. It
// returns the callback response; the browser's cookie jar now holds the session.
func signIn(t *testing.T, browser *http.Client, base string, idp *idptest.IDP) reply {
	t.Helper()
	login := get(t, browser, base+"/auth/login")
	providerURL, err := url.Parse(login.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse provider redirect: %v", err)
	}
	code := idp.IssueCode(providerURL.Query().Get("code_challenge"),
		idptest.Claims{Subject: "sub-1", Email: testOwnerEmail, EmailVerified: true, Name: "Owner"})
	return get(t, browser, base+"/auth/callback?"+url.Values{
		"code": {code}, "state": {providerURL.Query().Get("state")},
	}.Encode())
}

// fakeKagami is a kagami gRPC server that answers ListJobs and remembers the
// identity token torii sent.
type fakeKagami struct {
	kagamiv1.UnimplementedKagamiServiceServer
	mu       sync.Mutex
	identity string
}

func (f *fakeKagami) ListJobs(ctx context.Context, _ *kagamiv1.ListJobsRequest) (*kagamiv1.ListJobsResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if values := md.Get("x-shogun-identity"); len(values) > 0 {
		f.identity = values[0]
	}
	return &kagamiv1.ListJobsResponse{Jobs: []*kagamiv1.Job{
		{Id: "job-1", Title: "SRE", CompanyName: "Tessellate", Status: kagamiv1.JobStatus_JOB_STATUS_SAVED},
	}}, nil
}

func (f *fakeKagami) identityToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.identity
}

// fakeSoroban answers ListBudgets with one budget and keeps the identity token
// the call carried.
type fakeSoroban struct {
	sorobanv1.UnimplementedSorobanServiceServer
	mu       sync.Mutex
	identity string
}

func (f *fakeSoroban) ListBudgets(ctx context.Context, _ *sorobanv1.ListBudgetsRequest) (*sorobanv1.ListBudgetsResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if values := md.Get("x-shogun-identity"); len(values) > 0 {
		f.identity = values[0]
	}
	return &sorobanv1.ListBudgetsResponse{Budgets: []*sorobanv1.Budget{{
		Id: "budget-1", ScopeType: sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL, Period: sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY,
		LimitMicros: 20_000_000, Mode: sorobanv1.BudgetMode_BUDGET_MODE_HARD, Enabled: true, Version: 1,
		SpentMicros: 1_500_000, ResetsAt: timestamppb.New(time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC)),
	}}}, nil
}

func (f *fakeSoroban) identityToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.identity
}

func startFakeSoroban(t *testing.T) (*fakeSoroban, string) {
	t.Helper()
	lis := listen(t)
	srv := grpc.NewServer()
	fake := &fakeSoroban{}
	sorobanv1.RegisterSorobanServiceServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return fake, lis.Addr().String()
}

func startFakeKagami(t *testing.T) (*fakeKagami, string) {
	t.Helper()
	lis := listen(t)
	srv := grpc.NewServer()
	fake := &fakeKagami{}
	kagamiv1.RegisterKagamiServiceServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return fake, lis.Addr().String()
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

	// Act
	callback := signIn(t, browser, base, idp)
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

func TestRunBoardCallsKagamiAsTheSignedInOwner(t *testing.T) {
	// Arrange
	kagami, kagamiAddr := startFakeKagami(t)
	base, idp := startTorii(t, map[string]string{"KAGAMI_ADDR": kagamiAddr})
	browser := newBrowser(t)
	signIn(t, browser, base, idp)
	jobs := apiv1connect.NewJobsServiceClient(browser, base)

	// Act
	resp, err := jobs.GetBoard(context.Background(), connect.NewRequest(&apiv1.GetBoardRequest{}))
	// Assert
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	saved := resp.Msg.GetColumns()[0]
	if saved.GetStatus() != apiv1.JobStatus_JOB_STATUS_SAVED || len(saved.GetJobs()) != 1 || saved.GetJobs()[0].GetCompanyName() != "Tessellate" {
		t.Fatalf("saved column = %v", saved)
	}
	authority, authErr := authz.New([]byte(testIdentityKey))
	if authErr != nil {
		t.Fatal(authErr)
	}
	identity, verifyErr := authority.Verify(kagami.identityToken())
	if verifyErr != nil || identity.OwnerID == "" {
		t.Fatalf("kagami got identity %+v, verify error %v; want a valid token naming the owner", identity, verifyErr)
	}
}

func TestRunListBudgetsCallsSorobanAsTheSignedInOwner(t *testing.T) {
	// Arrange
	soroban, sorobanAddr := startFakeSoroban(t)
	base, idp := startTorii(t, map[string]string{"SOROBAN_ADDR": sorobanAddr})
	browser := newBrowser(t)
	signIn(t, browser, base, idp)
	costs := apiv1connect.NewCostsServiceClient(browser, base)

	// Act
	resp, err := costs.ListBudgets(context.Background(), connect.NewRequest(&apiv1.ListBudgetsRequest{}))
	// Assert
	if err != nil {
		t.Fatalf("ListBudgets: %v", err)
	}
	budgets := resp.Msg.GetBudgets()
	if len(budgets) != 1 || budgets[0].GetLimitMicros() != 20_000_000 || budgets[0].GetSpentMicros() != 1_500_000 ||
		budgets[0].GetScope() != apiv1.BudgetScope_BUDGET_SCOPE_GLOBAL || budgets[0].GetResetsAt() != "2026-10-31T18:30:00Z" {
		t.Fatalf("budgets = %v", budgets)
	}
	authority, authErr := authz.New([]byte(testIdentityKey))
	if authErr != nil {
		t.Fatal(authErr)
	}
	identity, verifyErr := authority.Verify(soroban.identityToken())
	if verifyErr != nil || identity.OwnerID == "" {
		t.Fatalf("soroban got identity %+v, verify error %v; want a valid token naming the owner", identity, verifyErr)
	}
}

func TestRunCostsWithoutSessionAreUnauthenticated(t *testing.T) {
	// Arrange
	base, _ := startTorii(t, nil)
	costs := apiv1connect.NewCostsServiceClient(newBrowser(t), base)

	// Act
	_, err := costs.ListBudgets(context.Background(), connect.NewRequest(&apiv1.ListBudgetsRequest{}))

	// Assert
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("code = %v, want Unauthenticated (err %v)", connect.CodeOf(err), err)
	}
}

func TestRunJobsWithoutSessionAreUnauthenticated(t *testing.T) {
	// Arrange
	base, _ := startTorii(t, nil)
	jobs := apiv1connect.NewJobsServiceClient(newBrowser(t), base)

	// Act
	_, err := jobs.GetBoard(context.Background(), connect.NewRequest(&apiv1.GetBoardRequest{}))

	// Assert
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("code = %v, want Unauthenticated (err %v)", connect.CodeOf(err), err)
	}
}

func TestRunContactsWithoutSessionAreUnauthenticated(t *testing.T) {
	// Arrange
	base, _ := startTorii(t, nil)
	contacts := apiv1connect.NewContactsServiceClient(newBrowser(t), base)

	// Act
	_, err := contacts.ListContacts(context.Background(), connect.NewRequest(&apiv1.ListContactsRequest{}))

	// Assert
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("code = %v, want Unauthenticated (err %v)", connect.CodeOf(err), err)
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
