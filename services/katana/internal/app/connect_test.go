package app_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

// fakeOAuth stands in for GitHub's authorization-code flow.
type fakeOAuth struct {
	token       string
	login       string
	exchangeErr error
	loginErr    error
	codes       []string
	tokensSeen  []string
}

func (f *fakeOAuth) AuthURL(state string) string {
	return "https://github.com/login/oauth/authorize?state=" + url.QueryEscape(state)
}

func (f *fakeOAuth) Exchange(_ context.Context, code string) (string, error) {
	f.codes = append(f.codes, code)
	return f.token, f.exchangeErr
}

func (f *fakeOAuth) Login(_ context.Context, token string) (string, error) {
	f.tokensSeen = append(f.tokensSeen, token)
	return f.login, f.loginErr
}

func goodOAuth() *fakeOAuth {
	return &fakeOAuth{token: "gho_granted-token-1", login: "octocat"}
}

func newConnector(t *testing.T, oauth app.OAuth, now func() time.Time) *app.Service {
	t.Helper()
	return newSvc(t, newPool(t), nil, now, app.WithOAuth(oauth))
}

func fixedNow() time.Time { return testNow }

// stateOf pulls the state out of the URL ConnectGitHub returned.
func stateOf(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil || u.Query().Get("state") == "" {
		t.Fatalf("auth url %q, err %v", authURL, err)
	}
	return u.Query().Get("state")
}

func TestConnectThenCompleteStoresTheOwnersSealedAccount(t *testing.T) {
	oauth := goodOAuth()
	svc := newConnector(t, oauth, fixedNow)
	owner := uuid.New()

	authURL, err := svc.ConnectGitHub(asOwner(owner))
	if err != nil {
		t.Fatalf("ConnectGitHub: %v", err)
	}
	view, err := svc.CompleteGitHubConnect(asOwner(owner), "the-code", stateOf(t, authURL))

	if err != nil || view.Login != "octocat" {
		t.Fatalf("view %+v, err %v", view, err)
	}
	if len(oauth.codes) != 1 || oauth.codes[0] != "the-code" || oauth.tokensSeen[0] != "gho_granted-token-1" {
		t.Fatalf("exchange saw codes %v, login saw tokens %v", oauth.codes, oauth.tokensSeen)
	}
	got, err := svc.GitHubAccount(asOwner(owner))
	if err != nil || got.Login != "octocat" {
		t.Fatalf("the account was not stored: %+v, %v", got, err)
	}
}

func TestTheStateNeverContainsTheOwner(t *testing.T) {
	svc := newConnector(t, goodOAuth(), fixedNow)
	owner := uuid.New()

	authURL, err := svc.ConnectGitHub(asOwner(owner))

	if err != nil || strings.Contains(authURL, owner.String()) {
		t.Fatalf("err %v; the URL exposes the owner: %q", err, authURL)
	}
}

func TestAStateIsOnlyGoodForTheOwnerItWasIssuedTo(t *testing.T) {
	oauth := goodOAuth()
	svc := newConnector(t, oauth, fixedNow)
	ada, bob := uuid.New(), uuid.New()
	authURL, _ := svc.ConnectGitHub(asOwner(ada))

	_, err := svc.CompleteGitHubConnect(asOwner(bob), "code", stateOf(t, authURL))

	if !errors.Is(err, app.ErrInvalidState) || len(oauth.codes) != 0 {
		t.Fatalf("err %v, %d exchanges; want bob refused before GitHub is called", err, len(oauth.codes))
	}
	if _, err := svc.GitHubAccount(asOwner(bob)); !errors.Is(err, app.ErrGitHubNotConnected) {
		t.Fatalf("bob got an account from ada's state: %v", err)
	}
}

func TestAStateExpiresAfterTenMinutes(t *testing.T) {
	now := testNow
	svc := newConnector(t, goodOAuth(), func() time.Time { return now })
	owner := uuid.New()
	fresh, _ := svc.ConnectGitHub(asOwner(owner))
	stale, _ := svc.ConnectGitHub(asOwner(owner))

	now = testNow.Add(9 * time.Minute)
	_, soon := svc.CompleteGitHubConnect(asOwner(owner), "code", stateOf(t, fresh))
	now = testNow.Add(11 * time.Minute)
	_, late := svc.CompleteGitHubConnect(asOwner(owner), "code", stateOf(t, stale))

	if soon != nil {
		t.Fatalf("a state nine minutes old was refused: %v", soon)
	}
	if !errors.Is(late, app.ErrInvalidState) {
		t.Fatalf("a state eleven minutes old: %v, want ErrInvalidState", late)
	}
}

func TestForgedAndGarbageStatesAreRefusedBeforeGitHubIsCalled(t *testing.T) {
	oauth := goodOAuth()
	svc := newConnector(t, oauth, fixedNow)
	owner := uuid.New()
	issued, _ := svc.ConnectGitHub(asOwner(owner))
	good := stateOf(t, issued)
	for name, state := range map[string]string{
		"empty":             "",
		"no separator":      "nonsense",
		"unknown key id":    "other-key" + good[strings.LastIndex(good, "."):],
		"truncated":         good[:len(good)-6],
		"a bit flipped":     good[:len(good)-2] + "AA",
		"not base64":        "katana-test.!!!",
		"empty sealed part": "katana-test.",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.CompleteGitHubConnect(asOwner(owner), "code", state)

			if !errors.Is(err, app.ErrInvalidState) {
				t.Fatalf("err = %v, want ErrInvalidState", err)
			}
		})
	}
	if len(oauth.codes) != 0 {
		t.Fatalf("GitHub was called %d times for a bad state", len(oauth.codes))
	}
}

func TestACodeGitHubRefusesIsReportedAsSuchAndStoresNothing(t *testing.T) {
	oauth := goodOAuth()
	oauth.exchangeErr = domain.ErrCodeRefused
	svc := newConnector(t, oauth, fixedNow)
	owner := uuid.New()
	authURL, _ := svc.ConnectGitHub(asOwner(owner))

	_, err := svc.CompleteGitHubConnect(asOwner(owner), "stale", stateOf(t, authURL))

	if !errors.Is(err, app.ErrInvalidCode) {
		t.Fatalf("err = %v, want ErrInvalidCode", err)
	}
	if _, err := svc.GitHubAccount(asOwner(owner)); !errors.Is(err, app.ErrGitHubNotConnected) {
		t.Fatalf("an account was stored for a refused code: %v", err)
	}
}

func TestACompleteWithoutACodeIsRefused(t *testing.T) {
	oauth := goodOAuth()
	svc := newConnector(t, oauth, fixedNow)
	owner := uuid.New()
	authURL, _ := svc.ConnectGitHub(asOwner(owner))

	_, err := svc.CompleteGitHubConnect(asOwner(owner), "", stateOf(t, authURL))

	if !errors.Is(err, app.ErrInvalidCode) || len(oauth.codes) != 0 {
		t.Fatalf("err %v, %d exchanges", err, len(oauth.codes))
	}
}

func TestWhenGitHubCannotSayWhoTheTokenIsNothingIsStored(t *testing.T) {
	oauth := goodOAuth()
	oauth.loginErr = domain.ErrUnavailable
	svc := newConnector(t, oauth, fixedNow)
	owner := uuid.New()
	authURL, _ := svc.ConnectGitHub(asOwner(owner))

	_, err := svc.CompleteGitHubConnect(asOwner(owner), "code", stateOf(t, authURL))

	if err == nil || errors.Is(err, app.ErrInvalidCode) {
		t.Fatalf("err = %v, want a failure that is not a refused code", err)
	}
	if _, err := svc.GitHubAccount(asOwner(owner)); !errors.Is(err, app.ErrGitHubNotConnected) {
		t.Fatalf("an account was stored without a login: %v", err)
	}
}

func TestConnectingAgainReplacesTheEarlierAccount(t *testing.T) {
	oauth := goodOAuth()
	svc := newConnector(t, oauth, fixedNow)
	owner := uuid.New()
	first, _ := svc.ConnectGitHub(asOwner(owner))
	if _, err := svc.CompleteGitHubConnect(asOwner(owner), "c1", stateOf(t, first)); err != nil {
		t.Fatalf("first: %v", err)
	}
	oauth.login, oauth.token = "octocat-two", "gho_second-token-2"
	second, _ := svc.ConnectGitHub(asOwner(owner))

	_, err := svc.CompleteGitHubConnect(asOwner(owner), "c2", stateOf(t, second))

	got, _ := svc.GitHubAccount(asOwner(owner))
	if err != nil || got.Login != "octocat-two" {
		t.Fatalf("err %v, account %+v; want the new login", err, got)
	}
}

func TestConnectIsRefusedWithoutAnOAuthAppAKeyringOrAnOwner(t *testing.T) {
	pool := newPool(t)
	owner := uuid.New()

	_, noApp := newSvc(t, pool, nil, nil).ConnectGitHub(asOwner(owner))
	_, noKeys := app.NewService(pool, nil, nil, app.WithOAuth(goodOAuth())).ConnectGitHub(asOwner(owner))
	_, noOwner := newSvc(t, pool, nil, nil, app.WithOAuth(goodOAuth())).ConnectGitHub(context.Background())

	if !errors.Is(noApp, app.ErrGitHubNotConfigured) {
		t.Errorf("without an OAuth app: %v, want ErrGitHubNotConfigured", noApp)
	}
	if !errors.Is(noKeys, app.ErrGitHubStorageOff) {
		t.Errorf("without a keyring: %v, want ErrGitHubStorageOff", noKeys)
	}
	if !errors.Is(noOwner, app.ErrNoOwner) {
		t.Errorf("without an owner: %v, want ErrNoOwner", noOwner)
	}
}
