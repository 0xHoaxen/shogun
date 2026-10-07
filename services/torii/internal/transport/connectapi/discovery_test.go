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
	shinobiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/shinobi/v1"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

// fakeShinobi records the last request of each RPC and answers with err, or
// with canned data.
type fakeShinobi struct {
	err error

	list    *shinobiv1.ListPostingsRequest
	save    *shinobiv1.SaveToTrackerRequest
	upsert  *shinobiv1.UpsertSourceRequest
	run     *shinobiv1.RunSourceRequest
	setPref *shinobiv1.SetPreferencesRequest
}

var cannedSource = &shinobiv1.Source{
	Id: "src1", Name: "Feed", Kind: shinobiv1.SourceKind_SOURCE_KIND_API, Schedule: "0 7 * * *", Enabled: true, LastError: "robots_disallowed",
	LastRunAt: timestamppb.New(time.Date(2026, 10, 7, 1, 30, 0, 0, time.UTC)),
	Config: &shinobiv1.SourceConfig{
		Url:     "https://jobs.example.com/api",
		Mapping: &shinobiv1.FieldMapping{ItemsPath: "data", Id: "id", Title: "title", Company: "company.name"},
	},
}

func (f *fakeShinobi) ListPostings(_ context.Context, in *shinobiv1.ListPostingsRequest, _ ...grpc.CallOption) (*shinobiv1.ListPostingsResponse, error) {
	f.list = in
	return &shinobiv1.ListPostingsResponse{NextPageToken: "next", Postings: []*shinobiv1.Posting{{
		Id: "p1", SourceId: "src1", Title: "Backend Engineer", Company: "Acme", Url: "https://acme.example/1", Location: "Remote",
		Scored: true, Score: 0.82, Reasons: []string{"role matches"}, ScoredBy: "llm", SavedJobId: "job1",
		PostedAt: timestamppb.New(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)), CreatedAt: timestamppb.New(time.Date(2026, 10, 7, 1, 30, 0, 0, time.UTC)),
	}}}, f.err
}

func (f *fakeShinobi) SaveToTracker(_ context.Context, in *shinobiv1.SaveToTrackerRequest, _ ...grpc.CallOption) (*shinobiv1.SaveToTrackerResponse, error) {
	f.save = in
	return &shinobiv1.SaveToTrackerResponse{JobId: "job1"}, f.err
}

func (f *fakeShinobi) ListSources(context.Context, *shinobiv1.ListSourcesRequest, ...grpc.CallOption) (*shinobiv1.ListSourcesResponse, error) {
	return &shinobiv1.ListSourcesResponse{Sources: []*shinobiv1.Source{cannedSource}}, f.err
}

func (f *fakeShinobi) UpsertSource(_ context.Context, in *shinobiv1.UpsertSourceRequest, _ ...grpc.CallOption) (*shinobiv1.UpsertSourceResponse, error) {
	f.upsert = in
	return &shinobiv1.UpsertSourceResponse{Source: cannedSource}, f.err
}

func (f *fakeShinobi) RunSource(_ context.Context, in *shinobiv1.RunSourceRequest, _ ...grpc.CallOption) (*shinobiv1.RunSourceResponse, error) {
	f.run = in
	return &shinobiv1.RunSourceResponse{Fetched: 12, Added: 5}, f.err
}

func (f *fakeShinobi) GetPreferences(context.Context, *shinobiv1.GetPreferencesRequest, ...grpc.CallOption) (*shinobiv1.GetPreferencesResponse, error) {
	return &shinobiv1.GetPreferencesResponse{Preferences: &shinobiv1.Preferences{
		Roles: []string{"backend engineer"}, Locations: []string{"remote"}, MustHave: []string{"go"}, NiceToHave: []string{"grpc"},
		Exclude: []string{"unpaid"}, MinScore: 0.7,
	}}, f.err
}

func (f *fakeShinobi) SetPreferences(_ context.Context, in *shinobiv1.SetPreferencesRequest, _ ...grpc.CallOption) (*shinobiv1.SetPreferencesResponse, error) {
	f.setPref = in
	return &shinobiv1.SetPreferencesResponse{Preferences: in.GetPreferences()}, f.err
}

func newDiscoveryHarness(t *testing.T) (*fakeShinobi, apiv1connect.DiscoveryServiceClient, string) {
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
	fake := &fakeShinobi{}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewDiscoveryServiceHandler(connectapi.NewDiscoveryServer(fake, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fake, apiv1connect.NewDiscoveryServiceClient(srv.Client(), srv.URL), token
}

func TestListDiscoveryPostingsPassesTheFiltersAndMapsThePostings(t *testing.T) {
	fake, client, token := newDiscoveryHarness(t)

	resp, err := client.ListDiscoveryPostings(context.Background(), withCookie(connect.NewRequest(&apiv1.ListDiscoveryPostingsRequest{
		MinScore: 0.5, SourceId: "src1", PageSize: 20, PageToken: "tok",
	}), token))

	if err != nil || fake.list.GetMinScore() != 0.5 || fake.list.GetSourceId() != "src1" || fake.list.GetPageSize() != 20 || fake.list.GetPageToken() != "tok" {
		t.Fatalf("err %v, shinobi got %+v", err, fake.list)
	}
	p := resp.Msg.GetPostings()[0]
	if p.GetId() != "p1" || p.GetTitle() != "Backend Engineer" || p.GetCompany() != "Acme" || !p.GetScored() || p.GetScore() != 0.82 ||
		p.GetReasons()[0] != "role matches" || p.GetScoredBy() != "llm" || p.GetSavedJobId() != "job1" || p.GetPostedAt() == nil ||
		resp.Msg.GetNextPageToken() != "next" {
		t.Fatalf("got %+v", resp.Msg)
	}
}

func TestSaveDiscoveryPostingPassesTheIDAndReturnsTheJob(t *testing.T) {
	fake, client, token := newDiscoveryHarness(t)

	resp, err := client.SaveDiscoveryPosting(context.Background(), withCookie(connect.NewRequest(&apiv1.SaveDiscoveryPostingRequest{PostingId: "p1"}), token))

	if err != nil || resp.Msg.GetJobId() != "job1" || fake.save.GetPostingId() != "p1" {
		t.Fatalf("got %v, %v, shinobi got %+v", resp, err, fake.save)
	}
}

func TestSourcesRoundTripTheirKindMappingAndLastRun(t *testing.T) {
	fake, client, token := newDiscoveryHarness(t)

	listed, listErr := client.ListDiscoverySources(context.Background(), withCookie(connect.NewRequest(&apiv1.ListDiscoverySourcesRequest{}), token))
	saved, saveErr := client.SaveDiscoverySource(context.Background(), withCookie(connect.NewRequest(&apiv1.SaveDiscoverySourceRequest{
		Source: &apiv1.DiscoverySource{
			Id: "src1", Name: "Feed", Kind: apiv1.DiscoverySourceKind_DISCOVERY_SOURCE_KIND_FILE, Schedule: "0 9 * * *", Enabled: true,
			Config: &apiv1.DiscoverySourceConfig{
				Document: `[]`, Mapping: &apiv1.DiscoveryFieldMapping{ItemsPath: "data", Id: "id", Title: "title", PostedAt: "date"},
			},
		},
	}), token))

	if listErr != nil || saveErr != nil {
		t.Fatalf("errors: %v %v", listErr, saveErr)
	}
	src := listed.Msg.GetSources()[0]
	if src.GetKind() != apiv1.DiscoverySourceKind_DISCOVERY_SOURCE_KIND_API || src.GetLastError() != "robots_disallowed" || src.GetLastRunAt() == nil ||
		src.GetConfig().GetUrl() != "https://jobs.example.com/api" || src.GetConfig().GetMapping().GetCompany() != "company.name" || !src.GetEnabled() {
		t.Fatalf("listed = %+v", src)
	}
	sent := fake.upsert.GetSource()
	if sent.GetId() != "src1" || sent.GetKind() != shinobiv1.SourceKind_SOURCE_KIND_FILE || sent.GetSchedule() != "0 9 * * *" ||
		sent.GetConfig().GetDocument() != `[]` || sent.GetConfig().GetMapping().GetPostedAt() != "date" || !sent.GetEnabled() {
		t.Fatalf("shinobi got %+v", sent)
	}
	if saved.Msg.GetSource().GetId() != "src1" {
		t.Fatalf("saved = %+v", saved.Msg)
	}
}

func TestRunDiscoverySourceReturnsTheCounts(t *testing.T) {
	fake, client, token := newDiscoveryHarness(t)

	resp, err := client.RunDiscoverySource(context.Background(), withCookie(connect.NewRequest(&apiv1.RunDiscoverySourceRequest{Id: "src1"}), token))

	if err != nil || resp.Msg.GetFetched() != 12 || resp.Msg.GetAdded() != 5 || fake.run.GetId() != "src1" {
		t.Fatalf("got %v, %v", resp, err)
	}
}

func TestPreferencesRoundTrip(t *testing.T) {
	fake, client, token := newDiscoveryHarness(t)

	got, getErr := client.GetDiscoveryPreferences(context.Background(), withCookie(connect.NewRequest(&apiv1.GetDiscoveryPreferencesRequest{}), token))
	set, setErr := client.SetDiscoveryPreferences(context.Background(), withCookie(connect.NewRequest(&apiv1.SetDiscoveryPreferencesRequest{
		Preferences: &apiv1.DiscoveryPreferences{Roles: []string{"sre"}, Exclude: []string{"unpaid"}, MinScore: 0.6},
	}), token))

	p := got.Msg.GetPreferences()
	if getErr != nil || setErr != nil || p.GetRoles()[0] != "backend engineer" || p.GetMustHave()[0] != "go" || p.GetNiceToHave()[0] != "grpc" || p.GetMinScore() != 0.7 {
		t.Fatalf("get = %+v, errors %v %v", p, getErr, setErr)
	}
	if sent := fake.setPref.GetPreferences(); sent.GetRoles()[0] != "sre" || sent.GetExclude()[0] != "unpaid" || sent.GetMinScore() != 0.6 ||
		set.Msg.GetPreferences().GetMinScore() != 0.6 {
		t.Fatalf("shinobi got %+v", sent)
	}
}

func TestDiscoveryErrorsKeepTheirReasonsAndHideInternals(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   connect.Code
		wantReason string
	}{
		{"source cannot be read", withInfo(codes.FailedPrecondition, "SOURCE_ROBOTS_DISALLOWED"), connect.CodeFailedPrecondition, "SOURCE_ROBOTS_DISALLOWED"},
		{"posting not savable", withInfo(codes.FailedPrecondition, "POSTING_NOT_SAVABLE"), connect.CodeFailedPrecondition, "POSTING_NOT_SAVABLE"},
		{"bad input", withInfo(codes.InvalidArgument, "INVALID_ARGUMENT"), connect.CodeInvalidArgument, "INVALID_ARGUMENT"},
		{"not found", withInfo(codes.NotFound, "SOURCE_NOT_FOUND"), connect.CodeNotFound, "SOURCE_NOT_FOUND"},
		{"too many", withInfo(codes.ResourceExhausted, "TOO_MANY_SOURCES"), connect.CodeResourceExhausted, "TOO_MANY_SOURCES"},
		{"tracker down", withInfo(codes.Unavailable, "TRACKER_UNAVAILABLE"), connect.CodeUnavailable, "UNAVAILABLE"},
		{"internals", status.Error(codes.Internal, "secret internals"), connect.CodeInternal, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, client, token := newDiscoveryHarness(t)
			fake.err = tt.err

			_, err := client.RunDiscoverySource(context.Background(), withCookie(connect.NewRequest(&apiv1.RunDiscoverySourceRequest{Id: "src1"}), token))

			code, reason := codeAndReason(t, err)
			if code != tt.wantCode || reason != tt.wantReason || (err != nil && strings.Contains(err.Error(), "secret internals")) {
				t.Fatalf("got %s %q (%v), want %s %q", code, reason, err, tt.wantCode, tt.wantReason)
			}
		})
	}
}

func TestDiscoveryCallsRequireASession(t *testing.T) {
	fake, client, _ := newDiscoveryHarness(t)

	_, err := client.ListDiscoveryPostings(context.Background(), connect.NewRequest(&apiv1.ListDiscoveryPostingsRequest{}))

	if code, _ := codeAndReason(t, err); code != connect.CodeUnauthenticated || fake.list != nil {
		t.Fatalf("got %v, shinobi called: %v", err, fake.list != nil)
	}
}
