package grpc_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/hanko"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
)

const (
	sendSubject = "Hello Lumen"
	sendBody    = "Dear Lumen team,\nI would like to help."
)

var sendTo = []string{"jobs@lumen.example"}

// stamp describes a token; the zero value is a valid one for the harness owner.
type stamp struct {
	draftID  string
	version  int64
	subject  string
	body     string
	to       []string
	owner    string
	audience string
	issuer   string
	key      ed25519.PrivateKey
	kid      string
	issuedAt time.Time
}

// token mints a Hanko the way fude would, from the harness's key.
func (h *harness) token(t *testing.T, draftID string, over func(*stamp)) string {
	t.Helper()
	s := stamp{
		draftID: draftID, version: 1, subject: sendSubject, body: sendBody, to: sendTo, owner: h.owner,
		audience: "tsubame", issuer: "fude", key: h.hankoPriv, kid: hankoKeyID, issuedAt: h.clock(),
	}
	if over != nil {
		over(&s)
	}
	claims, err := hanko.NewClaims(s.issuedAt, hanko.Claims{
		Audience: s.audience, Issuer: s.issuer, Subject: s.owner, DraftID: s.draftID, Version: s.version,
		BodySHA256: hex.EncodeToString(hanko.BodyDigest(s.subject, s.body)),
		RcptSHA256: hex.EncodeToString(hanko.RecipientDigest(s.to)),
	})
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	tok, err := hanko.Sign(claims, s.key, s.kid)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return tok
}

func (h *harness) connectMail(t *testing.T) {
	t.Helper()
	if _, err := h.accounts.Connect(context.Background(), uuid.MustParse(h.owner), "gmail", "me@example.com", refreshToken); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

func (h *harness) send(t *testing.T, hankoToken, draftID string) (*tsubamev1.SendResponse, error) {
	t.Helper()
	return h.client.Send(h.ctx(t), &tsubamev1.SendRequest{
		Hanko: hankoToken, DraftId: draftID, Version: 1, To: sendTo, Subject: sendSubject, Body: sendBody,
		ContactId: "0194f0a0-0000-7000-8000-0000000000c1", JobId: "0194f0a0-0000-7000-8000-0000000000e1",
	})
}

func (h *harness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestSendSendsApprovedMailOnceAndRecordsIt(t *testing.T) {
	h := newHarness(t)
	h.connectMail(t)
	draft := uuid.NewString()

	res, err := h.send(t, h.token(t, draft, nil), draft)

	if err != nil || res.GetProviderMessageId() == "" {
		t.Fatalf("got %+v, %v", res, err)
	}
	if h.mail.SentCount() != 1 {
		t.Fatalf("sent %d mails, want 1", h.mail.SentCount())
	}
	out := h.mail.Sent[0]
	if out.From != "me@example.com" || len(out.To) != 1 || out.To[0] != "jobs@lumen.example" || out.Subject != sendSubject ||
		out.Body != sendBody || out.DraftID != draft+":1" {
		t.Fatalf("provider got %+v", out)
	}
	var status, providerID string
	if err := h.pool.QueryRow(context.Background(), `SELECT status, provider_message_id FROM sends`).Scan(&status, &providerID); err != nil ||
		status != "sent" || providerID != res.GetProviderMessageId() {
		t.Fatalf("send row: %q %q, %v", status, providerID, err)
	}
	if n := h.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.sent'`); n != 1 {
		t.Fatalf("got %d draft.sent events, want 1", n)
	}
}

func TestSendRefusesEverythingTheTokenDoesNotCover(t *testing.T) {
	other := ed25519PrivateKey(t)
	tests := []struct {
		name  string
		token func(h *harness, draft string, t *testing.T) string
		req   func(r *tsubamev1.SendRequest)
	}{
		{"text changed after approval", nil, func(r *tsubamev1.SendRequest) { r.Body = sendBody + " P.S. send me money" }},
		{"subject changed", nil, func(r *tsubamev1.SendRequest) { r.Subject = "Something else" }},
		{"a recipient added", nil, func(r *tsubamev1.SendRequest) { r.To = append(r.To, "evil@x.example") }},
		{"a different recipient", nil, func(r *tsubamev1.SendRequest) { r.To = []string{"evil@x.example"} }},
		{"another version", nil, func(r *tsubamev1.SendRequest) { r.Version = 2 }},
		{"another draft", nil, func(r *tsubamev1.SendRequest) { r.DraftId = uuid.NewString() }},
		{"no token", func(*harness, string, *testing.T) string { return "" }, nil},
		{"garbage token", func(*harness, string, *testing.T) string { return "v4.public.not-a-token" }, nil},
		{"signed with another key", func(h *harness, d string, t *testing.T) string {
			return h.token(t, d, func(s *stamp) { s.key = other })
		}, nil},
		{"unknown key id", func(h *harness, d string, t *testing.T) string {
			return h.token(t, d, func(s *stamp) { s.kid = "someone-else" })
		}, nil},
		{"wrong audience", func(h *harness, d string, t *testing.T) string {
			return h.token(t, d, func(s *stamp) { s.audience = "katana" })
		}, nil},
		{"wrong issuer", func(h *harness, d string, t *testing.T) string {
			return h.token(t, d, func(s *stamp) { s.issuer = "torii" })
		}, nil},
		{"expired", func(h *harness, d string, t *testing.T) string {
			return h.token(t, d, func(s *stamp) { s.issuedAt = h.clock().Add(-10 * time.Minute) })
		}, nil},
		{"issued for another owner", func(h *harness, d string, t *testing.T) string {
			return h.token(t, d, func(s *stamp) { s.owner = uuid.NewString() })
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.connectMail(t)
			draft := uuid.NewString()
			tok := h.token(t, draft, nil)
			if tt.token != nil {
				tok = tt.token(h, draft, t)
			}
			req := &tsubamev1.SendRequest{Hanko: tok, DraftId: draft, Version: 1, To: append([]string(nil), sendTo...), Subject: sendSubject, Body: sendBody}
			if tt.req != nil {
				tt.req(req)
			}

			_, err := h.client.Send(h.ctx(t), req)

			requireStatus(t, err, codes.PermissionDenied, "NOT_APPROVED")
			if h.mail.SentCount() != 0 || h.count(t, `SELECT count(*) FROM sends`) != 0 {
				t.Fatalf("a refused send went out or was recorded: sent %d", h.mail.SentCount())
			}
		})
	}
}

func TestATokenWorksOnceAndAVersionIsSentOnce(t *testing.T) {
	h := newHarness(t)
	h.connectMail(t)
	draft := uuid.NewString()
	tok := h.token(t, draft, nil)
	if _, err := h.send(t, tok, draft); err != nil {
		t.Fatalf("first send: %v", err)
	}

	_, reuse := h.send(t, tok, draft)
	_, again := h.send(t, h.token(t, draft, nil), draft) // a second valid token for the same version

	requireStatus(t, reuse, codes.PermissionDenied, "APPROVAL_ALREADY_USED")
	requireStatus(t, again, codes.PermissionDenied, "ALREADY_SENT")
	if h.mail.SentCount() != 1 {
		t.Fatalf("sent %d mails, want exactly 1", h.mail.SentCount())
	}
}

func TestConcurrentSendsOfOneTokenSendOnce(t *testing.T) {
	h := newHarness(t)
	h.connectMail(t)
	draft := uuid.NewString()
	tok := h.token(t, draft, nil)
	const callers = 8
	errs := make(chan error, callers)
	var wg sync.WaitGroup

	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.send(t, tok, draft)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	succeeded := 0
	for err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 || h.mail.SentCount() != 1 {
		t.Fatalf("%d calls succeeded and %d mails went out; want exactly 1 of each", succeeded, h.mail.SentCount())
	}
}

func TestAProviderFailureIsRecordedAnnouncedAndNeedsANewApproval(t *testing.T) {
	h := newHarness(t)
	h.connectMail(t)
	draft := uuid.NewString()
	tok := h.token(t, draft, nil)
	h.mail.SendErr = &mail.APIError{Status: 503, Message: "UNAVAILABLE"}

	_, err := h.send(t, tok, draft)

	requireStatus(t, err, codes.FailedPrecondition, "SEND_FAILED")
	var status, reason string
	if err := h.pool.QueryRow(context.Background(), `SELECT status, error FROM sends`).Scan(&status, &reason); err != nil || status != "failed" || reason != "provider_error" {
		t.Fatalf("send row: %q %q, %v", status, reason, err)
	}
	if h.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.send_failed'`) != 1 || h.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.sent'`) != 0 {
		t.Fatal("want one draft.send_failed and no draft.sent")
	}
	_, replay := h.send(t, tok, draft)
	requireStatus(t, replay, codes.PermissionDenied, "APPROVAL_ALREADY_USED")

	h.mail.SendErr = nil
	if _, err := h.send(t, h.token(t, draft, nil), draft); err != nil {
		t.Fatalf("a fresh approval must be able to send after a failure: %v", err)
	}
	if h.mail.SentCount() != 1 {
		t.Fatalf("sent %d mails", h.mail.SentCount())
	}
}

func TestARevokedAccountFailsTheSendAndAsksForReauth(t *testing.T) {
	h := newHarness(t)
	h.connectMail(t)
	draft := uuid.NewString()
	h.mail.SendErr = mail.ErrAuthRevoked

	_, err := h.send(t, h.token(t, draft, nil), draft)

	requireStatus(t, err, codes.FailedPrecondition, "SEND_FAILED")
	var reason, status string
	_ = h.pool.QueryRow(context.Background(), `SELECT error FROM sends`).Scan(&reason)
	_ = h.pool.QueryRow(context.Background(), `SELECT status FROM accounts`).Scan(&status)
	if reason != "auth_revoked" || status != "reauth_required" {
		t.Fatalf("reason %q, account status %q", reason, status)
	}
}

func TestWithoutAUsableAccountNothingIsSpent(t *testing.T) {
	h := newHarness(t)
	draft := uuid.NewString()
	tok := h.token(t, draft, nil)

	_, err := h.send(t, tok, draft)

	requireStatus(t, err, codes.FailedPrecondition, "NO_MAIL_ACCOUNT")
	if h.count(t, `SELECT count(*) FROM sends`) != 0 || h.mail.SentCount() != 0 {
		t.Fatal("a send without an account must record nothing")
	}
	h.connectMail(t)
	if _, err := h.send(t, tok, draft); err != nil {
		t.Fatalf("the same approval must still work once an account is connected: %v", err)
	}
}

func TestSendRejectsMalformedRequests(t *testing.T) {
	h := newHarness(t)
	h.connectMail(t)
	draft := uuid.NewString()
	tok := h.token(t, draft, nil)
	tests := []struct {
		name string
		req  *tsubamev1.SendRequest
		code codes.Code
		why  string
	}{
		{"bad draft id", &tsubamev1.SendRequest{Hanko: tok, DraftId: "nope", Version: 1, To: sendTo, Body: "b"}, codes.InvalidArgument, "INVALID_SEND"},
		{"no version", &tsubamev1.SendRequest{Hanko: tok, DraftId: draft, To: sendTo, Body: "b"}, codes.InvalidArgument, "INVALID_SEND"},
		{"no recipient", &tsubamev1.SendRequest{Hanko: tok, DraftId: draft, Version: 1, Body: "b"}, codes.InvalidArgument, "INVALID_SEND"},
		{"no body", &tsubamev1.SendRequest{Hanko: tok, DraftId: draft, Version: 1, To: sendTo}, codes.InvalidArgument, "INVALID_SEND"},
		{"no token", &tsubamev1.SendRequest{DraftId: draft, Version: 1, To: sendTo, Body: "b"}, codes.PermissionDenied, "NOT_APPROVED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.Send(h.ctx(t), tt.req)

			requireStatus(t, err, tt.code, tt.why)
		})
	}
	if h.mail.SentCount() != 0 {
		t.Fatal("a malformed request sent mail")
	}
}

func TestSendNeedsACallerIdentity(t *testing.T) {
	h := newHarness(t)
	h.connectMail(t)
	draft := uuid.NewString()

	_, err := h.client.Send(t.Context(), &tsubamev1.SendRequest{Hanko: h.token(t, draft, nil), DraftId: draft, Version: 1, To: sendTo, Subject: sendSubject, Body: sendBody})

	requireStatus(t, err, codes.Unauthenticated, "")
	if h.mail.SentCount() != 0 {
		t.Fatal("an unauthenticated call sent mail")
	}
}

func ed25519PrivateKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}
