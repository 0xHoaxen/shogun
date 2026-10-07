package grpc_test

import (
	"bytes"
	"context"
	"net"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	shinobiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/shinobi/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/fetch"
	shinobigrpc "github.com/0xHoaxen/shogun/services/shinobi/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/shinobi/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

const feedURL = "https://jobs.example.com/feed.xml"

const feedXML = `<rss version="2.0"><channel>
 <item><title>Backend Engineer</title><link>https://jobs.example.com/1</link><guid>1</guid></item>
 <item><title>Platform Engineer</title><link>https://jobs.example.com/2</link><guid>2</guid></item>
</channel></rss>`

type fakeFetcher struct {
	doc string
	err error
}

func (f *fakeFetcher) Get(context.Context, string) ([]byte, error) { return []byte(f.doc), f.err }

// fakeTracker answers AddJob with a fixed job id, or err, and records the keys.
type fakeTracker struct {
	jobID string
	err   error
	keys  []string
}

func (f *fakeTracker) AddJob(_ context.Context, in *kagamiv1.AddJobRequest, _ ...grpc.CallOption) (*kagamiv1.AddJobResponse, error) {
	f.keys = append(f.keys, in.GetIdempotencyKey())
	if f.err != nil {
		return nil, f.err
	}
	return &kagamiv1.AddJobResponse{Job: &kagamiv1.Job{Id: f.jobID}}, nil
}

type harness struct {
	tracker   *fakeTracker
	client    shinobiv1.ShinobiServiceClient
	pool      *pgxpool.Pool
	authority *authz.Authority
	fetcher   *fakeFetcher
	owner     string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "shinobi")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	authority, err := authz.New(bytes.Repeat([]byte("k"), authz.MinKeyLength))
	if err != nil {
		t.Fatalf("authority: %v", err)
	}
	fetcher := &fakeFetcher{doc: feedXML}
	tracker := &fakeTracker{jobID: uuid.NewString()}
	srv := grpc.NewServer(grpc.UnaryInterceptor(authority.UnaryServerInterceptor()))
	shinobiv1.RegisterShinobiServiceServer(srv, shinobigrpc.New(app.NewService(pool, fetcher, nil, nil, app.WithTracker(tracker))))
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &harness{tracker: tracker, client: shinobiv1.NewShinobiServiceClient(conn), pool: pool, authority: authority, fetcher: fetcher, owner: uuid.NewString()}
}

func (h *harness) ctxFor(t *testing.T, owner string) context.Context {
	t.Helper()
	token, err := h.authority.Sign(authz.Identity{OwnerID: owner, RequestID: "r"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return metadata.AppendToOutgoingContext(context.Background(), authz.Header, token)
}

func (h *harness) ctx(t *testing.T) context.Context { return h.ctxFor(t, h.owner) }

func requireStatus(t *testing.T, err error, code codes.Code, reason string) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok || st.Code() != code {
		t.Fatalf("got %v, want code %s", err, code)
	}
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok && info.GetReason() == reason {
			return
		}
	}
	t.Fatalf("got %v, want reason %s", err, reason)
}

func rssSource(name string) *shinobiv1.Source {
	return &shinobiv1.Source{
		Name: name, Kind: shinobiv1.SourceKind_SOURCE_KIND_RSS, Enabled: true,
		Config: &shinobiv1.SourceConfig{Url: feedURL},
	}
}

func (h *harness) add(t *testing.T, s *shinobiv1.Source) *shinobiv1.Source {
	t.Helper()
	res, err := h.client.UpsertSource(h.ctx(t), &shinobiv1.UpsertSourceRequest{Source: s})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	return res.GetSource()
}

func TestUpsertSourceCreatesWithTheDefaultScheduleAndNoEvent(t *testing.T) {
	h := newHarness(t)

	got := h.add(t, rssSource("Feed"))

	if _, err := uuid.Parse(got.GetId()); err != nil || got.GetName() != "Feed" || got.GetKind() != shinobiv1.SourceKind_SOURCE_KIND_RSS ||
		got.GetSchedule() != "0 7 * * *" || !got.GetEnabled() || got.GetConfig().GetUrl() != feedURL || got.GetLastRunAt() != nil {
		t.Fatalf("source = %+v", got)
	}
	var outbox int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox`).Scan(&outbox); err != nil || outbox != 0 {
		t.Fatalf("outbox rows = %d, %v; nothing consumes a source", outbox, err)
	}
}

func TestUpsertSourceUpdatesAndKeepsTheMappingOfAnAPISource(t *testing.T) {
	h := newHarness(t)
	created := h.add(t, &shinobiv1.Source{
		Name: "API", Kind: shinobiv1.SourceKind_SOURCE_KIND_API, Enabled: true,
		Config: &shinobiv1.SourceConfig{
			Url:     "https://jobs.example.com/api",
			Mapping: &shinobiv1.FieldMapping{ItemsPath: "data", Id: "id", Title: "title", Company: "company.name"},
		},
	})
	created.Name, created.Schedule, created.Enabled = "API 2", "0 9 * * *", false

	res, err := h.client.UpsertSource(h.ctx(t), &shinobiv1.UpsertSourceRequest{Source: created})

	got := res.GetSource()
	if err != nil || got.GetId() != created.GetId() || got.GetName() != "API 2" || got.GetSchedule() != "0 9 * * *" || got.GetEnabled() ||
		got.GetConfig().GetMapping().GetCompany() != "company.name" || got.GetConfig().GetMapping().GetItemsPath() != "data" {
		t.Fatalf("source = %+v, %v", got, err)
	}
}

func TestUpsertSourceRefusesBadRequests(t *testing.T) {
	h := newHarness(t)
	created := h.add(t, rssSource("Feed"))
	tests := []struct {
		name   string
		source *shinobiv1.Source
		code   codes.Code
		reason string
	}{
		{"no name", &shinobiv1.Source{Kind: shinobiv1.SourceKind_SOURCE_KIND_RSS, Config: &shinobiv1.SourceConfig{Url: feedURL}}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"insecure url", &shinobiv1.Source{Name: "x", Kind: shinobiv1.SourceKind_SOURCE_KIND_RSS, Config: &shinobiv1.SourceConfig{Url: "http://jobs.example.com"}}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"private url", &shinobiv1.Source{Name: "x", Kind: shinobiv1.SourceKind_SOURCE_KIND_RSS, Config: &shinobiv1.SourceConfig{Url: "https://10.0.0.5/feed"}}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"bad schedule", &shinobiv1.Source{Name: "x", Kind: shinobiv1.SourceKind_SOURCE_KIND_RSS, Schedule: "daily", Config: &shinobiv1.SourceConfig{Url: feedURL}}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"no kind", &shinobiv1.Source{Name: "x", Config: &shinobiv1.SourceConfig{Url: feedURL}}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"bad id", &shinobiv1.Source{Id: "nope", Name: "x", Kind: shinobiv1.SourceKind_SOURCE_KIND_RSS, Config: &shinobiv1.SourceConfig{Url: feedURL}}, codes.InvalidArgument, "INVALID_ID"},
		{"unknown id", &shinobiv1.Source{Id: uuid.NewString(), Name: "x", Kind: shinobiv1.SourceKind_SOURCE_KIND_RSS, Config: &shinobiv1.SourceConfig{Url: feedURL}}, codes.NotFound, "SOURCE_NOT_FOUND"},
		{"change of kind", &shinobiv1.Source{Id: created.GetId(), Name: "x", Kind: shinobiv1.SourceKind_SOURCE_KIND_FILE, Config: &shinobiv1.SourceConfig{
			Document: `[]`, Mapping: &shinobiv1.FieldMapping{Id: "id", Title: "t"},
		}}, codes.FailedPrecondition, "SOURCE_KIND_FIXED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.UpsertSource(h.ctx(t), &shinobiv1.UpsertSourceRequest{Source: tt.source})

			requireStatus(t, err, tt.code, tt.reason)
		})
	}
}

func TestListSourcesIsPerOwner(t *testing.T) {
	h := newHarness(t)
	h.add(t, rssSource("b"))
	h.add(t, rssSource("a"))
	if _, err := h.client.UpsertSource(h.ctxFor(t, uuid.NewString()), &shinobiv1.UpsertSourceRequest{Source: rssSource("theirs")}); err != nil {
		t.Fatalf("other owner: %v", err)
	}

	res, err := h.client.ListSources(h.ctx(t), &shinobiv1.ListSourcesRequest{})

	if err != nil || len(res.GetSources()) != 2 || res.GetSources()[0].GetName() != "a" || res.GetSources()[1].GetName() != "b" {
		t.Fatalf("res %v, err %v", res, err)
	}
}

func TestRunSourceReadsItAndReportsTheCounts(t *testing.T) {
	h := newHarness(t)
	src := h.add(t, rssSource("Feed"))

	first, err := h.client.RunSource(h.ctx(t), &shinobiv1.RunSourceRequest{Id: src.GetId()})
	second, _ := h.client.RunSource(h.ctx(t), &shinobiv1.RunSourceRequest{Id: src.GetId()})
	listed, _ := h.client.ListSources(h.ctx(t), &shinobiv1.ListSourcesRequest{})

	if err != nil || first.GetFetched() != 2 || first.GetAdded() != 2 || second.GetAdded() != 0 {
		t.Fatalf("first %v (%v), second %v", first, err, second)
	}
	if listed.GetSources()[0].GetLastRunAt() == nil || listed.GetSources()[0].GetLastError() != "" {
		t.Fatalf("source after runs = %+v", listed.GetSources()[0])
	}
}

func TestRunSourceFailuresHaveStableReasonsAndAreKeptWithTheSource(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		reason string
		code   string
	}{
		{"robots", fetch.ErrRobotsDisallowed, "SOURCE_ROBOTS_DISALLOWED", "robots_disallowed"},
		{"private", fetch.ErrNotPublic, "SOURCE_ADDRESS_NOT_PUBLIC", "address_not_public"},
		{"too large", fetch.ErrTooLarge, "SOURCE_TOO_LARGE", "too_large"},
		{"unreachable", fetch.ErrFailed, "SOURCE_FETCH_FAILED", "fetch_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.fetcher.err = tt.err
			src := h.add(t, rssSource("Feed"))

			_, err := h.client.RunSource(h.ctx(t), &shinobiv1.RunSourceRequest{Id: src.GetId()})

			requireStatus(t, err, codes.FailedPrecondition, tt.reason)
			listed, _ := h.client.ListSources(h.ctx(t), &shinobiv1.ListSourcesRequest{})
			if listed.GetSources()[0].GetLastError() != tt.code {
				t.Fatalf("last error = %q, want %q", listed.GetSources()[0].GetLastError(), tt.code)
			}
		})
	}
}

func TestRunSourceRefusesUnknownForeignAndMalformedIDs(t *testing.T) {
	h := newHarness(t)
	src := h.add(t, rssSource("Feed"))

	_, missing := h.client.RunSource(h.ctx(t), &shinobiv1.RunSourceRequest{Id: uuid.NewString()})
	_, foreign := h.client.RunSource(h.ctxFor(t, uuid.NewString()), &shinobiv1.RunSourceRequest{Id: src.GetId()})
	_, bad := h.client.RunSource(h.ctx(t), &shinobiv1.RunSourceRequest{Id: "nope"})

	requireStatus(t, missing, codes.NotFound, "SOURCE_NOT_FOUND")
	requireStatus(t, foreign, codes.NotFound, "SOURCE_NOT_FOUND")
	requireStatus(t, bad, codes.InvalidArgument, "INVALID_ID")
}

func TestSourceCallsRequireAnIdentity(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.ListSources(context.Background(), &shinobiv1.ListSourcesRequest{})

	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("code = %s, want Unauthenticated", got)
	}
}
