package grpc_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

type fakeOAuth struct {
	exchangeErr error
}

func (f *fakeOAuth) AuthURL(state string) string {
	return "https://github.com/login/oauth/authorize?state=" + url.QueryEscape(state)
}

func (f *fakeOAuth) Exchange(context.Context, string) (string, error) {
	return "gho_granted-token-1", f.exchangeErr
}

func (f *fakeOAuth) Login(context.Context, string) (string, error) { return "octocat", nil }

func stateIn(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil || u.Query().Get("state") == "" {
		t.Fatalf("authorization url %q, err %v", authURL, err)
	}
	return u.Query().Get("state")
}

func TestConnectThenCompleteThenGetThenDisconnect(t *testing.T) {
	h := newHarness(t, true, app.WithOAuth(&fakeOAuth{}))
	// A user with no connection yet.
	h.owner = uuid.NewString()

	before, err := h.client.GetGitHubAccount(h.ctx(t), &katanav1.GetGitHubAccountRequest{})
	conn, connErr := h.client.ConnectGitHub(h.ctx(t), &katanav1.ConnectGitHubRequest{})
	var done *katanav1.CompleteGitHubConnectResponse
	var doneErr error
	if connErr == nil {
		done, doneErr = h.client.CompleteGitHubConnect(h.ctx(t), &katanav1.CompleteGitHubConnectRequest{
			Code: "c", State: stateIn(t, conn.GetAuthorizationUrl()),
		})
	}
	after, afterErr := h.client.GetGitHubAccount(h.ctx(t), &katanav1.GetGitHubAccountRequest{})
	_, discErr := h.client.DisconnectGitHub(h.ctx(t), &katanav1.DisconnectGitHubRequest{})
	gone, goneErr := h.client.GetGitHubAccount(h.ctx(t), &katanav1.GetGitHubAccountRequest{})

	if err != nil || before.GetConnected() {
		t.Fatalf("before: %v, %v", before, err)
	}
	if connErr != nil || doneErr != nil || done.GetLogin() != "octocat" || done.GetConnectedAt() == nil {
		t.Fatalf("connect %v, complete %v (%v)", connErr, done, doneErr)
	}
	if afterErr != nil || !after.GetConnected() || after.GetLogin() != "octocat" {
		t.Fatalf("after: %v, %v", after, afterErr)
	}
	if discErr != nil || goneErr != nil || gone.GetConnected() || gone.GetLogin() != "" {
		t.Fatalf("after disconnect: %v, %v, %v", gone, discErr, goneErr)
	}
}

func TestCompleteMapsABadStateAndARefusedCodeToStableReasons(t *testing.T) {
	h := newHarness(t, true, app.WithOAuth(&fakeOAuth{exchangeErr: domain.ErrCodeRefused}))
	conn, err := h.client.ConnectGitHub(h.ctx(t), &katanav1.ConnectGitHubRequest{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	_, badState := h.client.CompleteGitHubConnect(h.ctx(t), &katanav1.CompleteGitHubConnectRequest{Code: "c", State: "forged"})
	_, badCode := h.client.CompleteGitHubConnect(h.ctx(t), &katanav1.CompleteGitHubConnectRequest{
		Code: "c", State: stateIn(t, conn.GetAuthorizationUrl()),
	})

	requireStatus(t, badState, codes.InvalidArgument, "INVALID_STATE")
	requireStatus(t, badCode, codes.InvalidArgument, "INVALID_CODE")
}

func TestConnectWithoutAnOAuthAppSaysItIsNotSetUp(t *testing.T) {
	h := newHarness(t, true)

	_, err := h.client.ConnectGitHub(h.ctx(t), &katanav1.ConnectGitHubRequest{})

	requireStatus(t, err, codes.FailedPrecondition, "GITHUB_NOT_CONFIGURED")
}

func TestTheConnectRPCsRequireAnIdentity(t *testing.T) {
	h := newHarness(t, true, app.WithOAuth(&fakeOAuth{}))
	ctx := context.Background()

	_, a := h.client.ConnectGitHub(ctx, &katanav1.ConnectGitHubRequest{})
	_, b := h.client.CompleteGitHubConnect(ctx, &katanav1.CompleteGitHubConnectRequest{})
	_, c := h.client.GetGitHubAccount(ctx, &katanav1.GetGitHubAccountRequest{})
	_, d := h.client.DisconnectGitHub(ctx, &katanav1.DisconnectGitHubRequest{})

	for i, err := range []error{a, b, c, d} {
		if got := status.Code(err); got != codes.Unauthenticated {
			t.Errorf("rpc %d: code = %s, want Unauthenticated", i, got)
		}
	}
}
