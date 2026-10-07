package grpc_test

import (
	"bytes"
	"context"
	"net"
	"os"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	katanagrpc "github.com/0xHoaxen/shogun/services/katana/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/katana/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

type fakeGitHub struct {
	snap domain.Snapshot
	err  error
}

func (f *fakeGitHub) Fetch(context.Context, string) (domain.Snapshot, error) { return f.snap, f.err }

type harness struct {
	client    katanav1.KatanaServiceClient
	authority *authz.Authority
	gh        *fakeGitHub
	owner     string
}

func newHarness(t *testing.T, configured bool) *harness {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "katana")
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
	gh := &fakeGitHub{snap: domain.Snapshot{ETag: "e", Repos: []domain.Repo{{Name: "r"}}}}
	var source app.GitHub
	if configured {
		source = gh
	}
	srv := grpc.NewServer(grpc.UnaryInterceptor(authority.UnaryServerInterceptor()))
	katanav1.RegisterKatanaServiceServer(srv, katanagrpc.New(app.NewService(pool, source, nil)))
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
	return &harness{client: katanav1.NewKatanaServiceClient(conn), authority: authority, gh: gh, owner: uuid.NewString()}
}

func (h *harness) ctx(t *testing.T) context.Context {
	t.Helper()
	token, err := h.authority.Sign(authz.Identity{OwnerID: h.owner, RequestID: "r"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return metadata.AppendToOutgoingContext(context.Background(), authz.Header, token)
}

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

func TestSyncGitHubStoresASnapshotAndReportsIt(t *testing.T) {
	h := newHarness(t, true)

	res, err := h.client.SyncGitHub(h.ctx(t), &katanav1.SyncGitHubRequest{})

	if err != nil || !res.GetChanged() {
		t.Fatalf("res %v, err %v", res, err)
	}
	if _, err := uuid.Parse(res.GetSnapshotId()); err != nil {
		t.Fatalf("snapshot id %q: %v", res.GetSnapshotId(), err)
	}
}

func TestSyncGitHubReportsNothingNewWithoutAnID(t *testing.T) {
	h := newHarness(t, true)
	h.gh.err = domain.ErrNotModified

	res, err := h.client.SyncGitHub(h.ctx(t), &katanav1.SyncGitHubRequest{})

	if err != nil || res.GetChanged() || res.GetSnapshotId() != "" {
		t.Fatalf("res %v, err %v", res, err)
	}
}

func TestSyncGitHubMapsFailuresToStableReasons(t *testing.T) {
	tests := []struct {
		name       string
		configured bool
		err        error
		code       codes.Code
		reason     string
	}{
		{"not configured", false, nil, codes.FailedPrecondition, "GITHUB_NOT_CONFIGURED"},
		{"token refused", true, domain.ErrUnauthorized, codes.FailedPrecondition, "GITHUB_UNAUTHORIZED"},
		{"rate limited", true, &domain.RateLimitError{}, codes.ResourceExhausted, "GITHUB_RATE_LIMITED"},
		{"github down", true, domain.ErrUnavailable, codes.Unavailable, "GITHUB_UNAVAILABLE"},
		{"anything else", true, context.DeadlineExceeded, codes.Internal, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.configured)
			h.gh.err = tt.err

			_, err := h.client.SyncGitHub(h.ctx(t), &katanav1.SyncGitHubRequest{})

			requireStatus(t, err, tt.code, tt.reason)
		})
	}
}

func TestSyncGitHubRequiresAnIdentity(t *testing.T) {
	h := newHarness(t, true)

	_, err := h.client.SyncGitHub(context.Background(), &katanav1.SyncGitHubRequest{})

	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("code = %s, want Unauthenticated", got)
	}
}
