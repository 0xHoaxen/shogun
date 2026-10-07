package connectapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

// fakeKatana records the last request of each RPC and answers with err, or with
// the canned suggestion.
type fakeKatana struct {
	err error

	sync    *katanav1.SyncGitHubRequest
	list    *katanav1.ListSuggestionsRequest
	accept  *katanav1.AcceptSuggestionRequest
	dismiss *katanav1.DismissSuggestionRequest
	changed bool
}

var cannedSuggestion = &katanav1.Suggestion{
	Id: "s1", Target: katanav1.SuggestionTarget_SUGGESTION_TARGET_LINKEDIN, Section: "skills", Before: "Go", After: "Go, Rust",
	Reason: "two Rust repos", State: katanav1.SuggestionState_SUGGESTION_STATE_OPEN,
	Evidence:  []*katanav1.Evidence{{Label: "repo b", Url: "https://github.com/o/b"}},
	CreatedAt: timestamppb.New(time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)),
}

func (f *fakeKatana) SyncGitHub(_ context.Context, in *katanav1.SyncGitHubRequest, _ ...grpc.CallOption) (*katanav1.SyncGitHubResponse, error) {
	f.sync = in
	return &katanav1.SyncGitHubResponse{Changed: f.changed}, f.err
}

func (f *fakeKatana) ListSuggestions(_ context.Context, in *katanav1.ListSuggestionsRequest, _ ...grpc.CallOption) (*katanav1.ListSuggestionsResponse, error) {
	f.list = in
	return &katanav1.ListSuggestionsResponse{Suggestions: []*katanav1.Suggestion{cannedSuggestion}, NextPageToken: "next"}, f.err
}

func (f *fakeKatana) AcceptSuggestion(_ context.Context, in *katanav1.AcceptSuggestionRequest, _ ...grpc.CallOption) (*katanav1.AcceptSuggestionResponse, error) {
	f.accept = in
	return &katanav1.AcceptSuggestionResponse{Suggestion: cannedSuggestion}, f.err
}

func (f *fakeKatana) DismissSuggestion(_ context.Context, in *katanav1.DismissSuggestionRequest, _ ...grpc.CallOption) (*katanav1.DismissSuggestionResponse, error) {
	f.dismiss = in
	return &katanav1.DismissSuggestionResponse{Suggestion: cannedSuggestion}, f.err
}

func newProfileHarness(t *testing.T) (*fakeKatana, apiv1connect.ProfileServiceClient, string) {
	t.Helper()
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	auth, err := app.NewAuth(apptest.NewMemSessions(),
		app.AuthConfig{AllowedEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	token, _, err := auth.StartSession(context.Background(), app.Claims{Subject: "sub-1", Email: ownerEmail, EmailVerified: true})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	fake := &fakeKatana{}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewProfileServiceHandler(connectapi.NewProfileServer(fake, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fake, apiv1connect.NewProfileServiceClient(srv.Client(), srv.URL), token
}

func TestListProfileSuggestionsMapsTheFiltersAndTheSuggestions(t *testing.T) {
	fake, client, token := newProfileHarness(t)

	resp, err := client.ListProfileSuggestions(context.Background(), withCookie(connect.NewRequest(&apiv1.ListProfileSuggestionsRequest{
		State: apiv1.SuggestionState_SUGGESTION_STATE_OPEN, Target: apiv1.SuggestionTarget_SUGGESTION_TARGET_LINKEDIN, PageSize: 10, PageToken: "tok",
	}), token))
	if err != nil {
		t.Fatalf("ListProfileSuggestions: %v", err)
	}
	if fake.list.GetState() != katanav1.SuggestionState_SUGGESTION_STATE_OPEN || fake.list.GetTarget() != katanav1.SuggestionTarget_SUGGESTION_TARGET_LINKEDIN ||
		fake.list.GetPageSize() != 10 || fake.list.GetPageToken() != "tok" {
		t.Fatalf("katana got %+v", fake.list)
	}
	got := resp.Msg.GetSuggestions()[0]
	if got.GetId() != "s1" || got.GetTarget() != apiv1.SuggestionTarget_SUGGESTION_TARGET_LINKEDIN || got.GetState() != apiv1.SuggestionState_SUGGESTION_STATE_OPEN ||
		got.GetSection() != "skills" || got.GetBefore() != "Go" || got.GetAfter() != "Go, Rust" || got.GetReason() != "two Rust repos" ||
		len(got.GetEvidence()) != 1 || got.GetEvidence()[0].GetUrl() != "https://github.com/o/b" || resp.Msg.GetNextPageToken() != "next" {
		t.Fatalf("got %+v", resp.Msg)
	}
}

func TestAcceptAndDismissPassTheIDAndReturnTheSuggestion(t *testing.T) {
	fake, client, token := newProfileHarness(t)

	a, aErr := client.AcceptProfileSuggestion(context.Background(), withCookie(connect.NewRequest(&apiv1.AcceptProfileSuggestionRequest{Id: "s1"}), token))
	d, dErr := client.DismissProfileSuggestion(context.Background(), withCookie(connect.NewRequest(&apiv1.DismissProfileSuggestionRequest{Id: "s2"}), token))

	if aErr != nil || dErr != nil || a.Msg.GetSuggestion().GetId() != "s1" || d.Msg.GetSuggestion().GetId() != "s1" {
		t.Fatalf("accept %v %v, dismiss %v %v", a, aErr, d, dErr)
	}
	if fake.accept.GetId() != "s1" || fake.dismiss.GetId() != "s2" {
		t.Fatalf("katana got accept %q, dismiss %q", fake.accept.GetId(), fake.dismiss.GetId())
	}
}

func TestSyncGitHubReportsWhetherAnythingChanged(t *testing.T) {
	fake, client, token := newProfileHarness(t)
	fake.changed = true

	resp, err := client.SyncGitHub(context.Background(), withCookie(connect.NewRequest(&apiv1.SyncGitHubRequest{}), token))

	if err != nil || !resp.Msg.GetChanged() || fake.sync == nil {
		t.Fatalf("got %v, %v", resp, err)
	}
}

func TestProfileErrorsKeepTheirReasonsAndHideInternals(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   connect.Code
		wantReason string
	}{
		{"already decided", withInfo(codes.FailedPrecondition, "SUGGESTION_ALREADY_DECIDED"), connect.CodeFailedPrecondition, "SUGGESTION_ALREADY_DECIDED"},
		{"not found", withInfo(codes.NotFound, "SUGGESTION_NOT_FOUND"), connect.CodeNotFound, "SUGGESTION_NOT_FOUND"},
		{"github not set", withInfo(codes.FailedPrecondition, "GITHUB_NOT_CONFIGURED"), connect.CodeFailedPrecondition, "GITHUB_NOT_CONFIGURED"},
		{"token refused", withInfo(codes.FailedPrecondition, "GITHUB_UNAUTHORIZED"), connect.CodeFailedPrecondition, "GITHUB_UNAUTHORIZED"},
		{"rate limited", withInfo(codes.ResourceExhausted, "GITHUB_RATE_LIMITED"), connect.CodeResourceExhausted, "GITHUB_RATE_LIMITED"},
		{"github down", withInfo(codes.Unavailable, "GITHUB_UNAVAILABLE"), connect.CodeUnavailable, "UNAVAILABLE"},
		{"internals", status.Error(codes.Internal, "secret internals"), connect.CodeInternal, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, client, token := newProfileHarness(t)
			fake.err = tt.err

			_, err := client.SyncGitHub(context.Background(), withCookie(connect.NewRequest(&apiv1.SyncGitHubRequest{}), token))

			code, reason := codeAndReason(t, err)
			if code != tt.wantCode || reason != tt.wantReason || (err != nil && strings.Contains(err.Error(), "secret internals")) {
				t.Fatalf("got %s %q (%v), want %s %q", code, reason, err, tt.wantCode, tt.wantReason)
			}
		})
	}
}

func TestProfileCallsRequireASession(t *testing.T) {
	fake, client, _ := newProfileHarness(t)

	_, err := client.ListProfileSuggestions(context.Background(), connect.NewRequest(&apiv1.ListProfileSuggestionsRequest{}))

	if code, _ := codeAndReason(t, err); code != connect.CodeUnauthenticated || fake.list != nil {
		t.Fatalf("got %v, katana called: %v", err, fake.list != nil)
	}
}
