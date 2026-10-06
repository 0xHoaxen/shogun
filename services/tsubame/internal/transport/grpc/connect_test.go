package grpc_test

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
)

func TestConnectThenCompleteStoresAnActiveAccountWithItsSealedToken(t *testing.T) {
	h := newHarness(t)
	ctx := h.ctx(t)
	authURL, state := h.begin(ctx, t)

	res, err := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: goodCode, State: state})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	acc := res.GetAccount()
	if acc.GetAddress() != "me@example.com" || acc.GetStatus() != tsubamev1.AccountStatus_ACCOUNT_STATUS_ACTIVE ||
		acc.GetProvider() != tsubamev1.MailProvider_MAIL_PROVIDER_GMAIL || acc.GetId() == "" {
		t.Fatalf("got %+v", acc)
	}
	id := uuid.MustParse(acc.GetId())
	got, err := h.accounts.Token(ctx, uuid.MustParse(h.owner), id)
	if err != nil || got != refreshToken {
		t.Fatalf("stored token %q, %v; want the one Google granted", got, err)
	}
	u, _ := url.Parse(authURL)
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" || state == "" {
		t.Fatalf("auth url %v", q)
	}
	if len(h.google.verifiers) != 1 || h.google.verifiers[0] == "" || strings.Contains(authURL, h.google.verifiers[0]) {
		t.Fatalf("the PKCE verifier must reach Google's token endpoint and never the URL: %v", h.google.verifiers)
	}
}

func TestConnectingTheSameMailboxAgainKeepsOneAccount(t *testing.T) {
	h := newHarness(t)
	ctx := h.ctx(t)
	_, first := h.begin(ctx, t)
	a, err := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: goodCode, State: first})
	if err != nil {
		t.Fatal(err)
	}
	h.google.mu.Lock()
	h.google.usedCodes = map[string]bool{}
	h.google.mu.Unlock()
	_, second := h.begin(ctx, t)

	b, err := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: goodCode, State: second})

	if err != nil || a.GetAccount().GetId() != b.GetAccount().GetId() {
		t.Fatalf("got %v and %v, %v; want the same account", a.GetAccount().GetId(), b.GetAccount().GetId(), err)
	}
	if list, _ := h.accounts.List(ctx, uuid.MustParse(h.owner)); len(list) != 1 {
		t.Fatalf("got %d accounts", len(list))
	}
}

func TestCompleteRefusesAStateThatIsNotValidForThisOwnerNow(t *testing.T) {
	tests := []struct {
		name  string
		state func(t *testing.T, h *harness, state string) (string, string) // state to send, owner to call as
	}{
		{"garbage", func(*testing.T, *harness, string) (string, string) { return "not-a-state", "" }},
		{"empty", func(*testing.T, *harness, string) (string, string) { return "", "" }},
		{"tampered", func(_ *testing.T, _ *harness, s string) (string, string) { return s[:len(s)-4] + "AAAA", "" }},
		{"another key id", func(_ *testing.T, _ *harness, s string) (string, string) {
			_, rest, _ := strings.Cut(s, ".")
			return "k9." + rest, ""
		}},
		{"expired", func(_ *testing.T, h *harness, s string) (string, string) {
			h.advance(11 * time.Minute)
			return s, ""
		}},
		{"issued to another owner", func(*testing.T, *harness, string) (string, string) { return "", uuid.NewString() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			_, state := h.begin(h.ctx(t), t)
			send, as := tt.state(t, h, state)
			if send == "" && as != "" {
				send = state
			}
			ctx := h.ctx(t)
			if as != "" {
				ctx = h.ctxFor(t, as)
			}

			_, err := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: goodCode, State: send})

			requireStatus(t, err, codes.InvalidArgument, "INVALID_STATE")
			if list, _ := h.accounts.List(ctx, uuid.MustParse(h.owner)); len(list) != 0 {
				t.Fatalf("a refused state stored %d accounts", len(list))
			}
			if len(h.google.verifiers) != 0 {
				t.Fatal("a refused state must not reach Google")
			}
		})
	}
}

func TestCompleteRefusesACodeGoogleRejectsOrHasSeen(t *testing.T) {
	h := newHarness(t)
	ctx := h.ctx(t)
	_, state := h.begin(ctx, t)

	_, badErr := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: "4/wrong", State: state})
	_, firstErr := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: goodCode, State: state})
	_, reusedErr := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: goodCode, State: state})
	_, emptyErr := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{State: state})

	requireStatus(t, badErr, codes.InvalidArgument, "INVALID_CODE")
	if firstErr != nil {
		t.Fatalf("a good code after a bad one: %v", firstErr)
	}
	requireStatus(t, reusedErr, codes.InvalidArgument, "INVALID_CODE")
	requireStatus(t, emptyErr, codes.InvalidArgument, "INVALID_CODE")
}

func TestConnectRefusesAnUnsupportedProviderAndCallsWithoutAnOwner(t *testing.T) {
	h := newHarness(t)

	_, providerErr := h.client.ConnectAccount(h.ctx(t), &tsubamev1.ConnectAccountRequest{})
	_, noIdentity := h.client.ConnectAccount(t.Context(), &tsubamev1.ConnectAccountRequest{Provider: tsubamev1.MailProvider_MAIL_PROVIDER_GMAIL})
	_, badOwner := h.client.ConnectAccount(h.ctxFor(t, "not-a-uuid"), &tsubamev1.ConnectAccountRequest{Provider: tsubamev1.MailProvider_MAIL_PROVIDER_GMAIL})

	requireStatus(t, providerErr, codes.InvalidArgument, "INVALID_PROVIDER")
	requireStatus(t, noIdentity, codes.Unauthenticated, "")
	requireStatus(t, badOwner, codes.PermissionDenied, "OWNER_REQUIRED")
}

func TestNoSecretReachesTheLogsOrAnErrorMessage(t *testing.T) {
	h := newHarness(t)
	ctx := h.ctx(t)
	authURL, state := h.begin(ctx, t)
	_, bad := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: "4/wrong-code", State: state})
	_, ok := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: goodCode, State: state})
	_, replay := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: goodCode, State: state})
	_, forged := h.client.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: goodCode, State: "forged-state"})
	if ok != nil {
		t.Fatalf("complete: %v", ok)
	}
	secrets := []string{goodCode, "4/wrong-code", refreshToken, accessToken, state, "forged-state", h.google.verifiers[0]}

	output := h.logs.String()
	for _, err := range []error{bad, replay, forged} {
		output += "\n" + err.Error()
	}
	base, _, _ := strings.Cut(authURL, "?")
	output += "\n" + base

	if leaked := contains(output, secrets...); leaked {
		t.Fatalf("a secret reached the logs or an error:\n%s", output)
	}
	if !strings.Contains(h.logs.String(), "mail account connected") {
		t.Fatalf("expected the connection to be logged without secrets:\n%s", h.logs.String())
	}
}
