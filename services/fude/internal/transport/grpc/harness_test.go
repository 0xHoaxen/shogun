package grpc_test

import (
	"bytes"
	"context"
	"net"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	fudegrpc "github.com/0xHoaxen/shogun/services/fude/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/fude/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

// harness is a fude server on an in-memory listener with the real authz
// interceptors, backed by a migrated Postgres schema.
type harness struct {
	client    fudev1.FudeServiceClient
	pool      *pgxpool.Pool
	authority *authz.Authority
	owner     string
	queue     *fakeQueue
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "fude")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	queue := &fakeQueue{}
	authority, err := authz.New(bytes.Repeat([]byte("k"), authz.MinKeyLength))
	if err != nil {
		t.Fatalf("authority: %v", err)
	}
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(authority.UnaryServerInterceptor()),
		grpc.StreamInterceptor(authority.StreamServerInterceptor()),
	)
	fudev1.RegisterFudeServiceServer(srv, fudegrpc.New(app.NewService(pool, queue, nil)))
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &harness{
		client:    fudev1.NewFudeServiceClient(conn),
		pool:      pool,
		authority: authority,
		owner:     uuid.NewString(),
		queue:     queue,
	}
}

// ctxFor returns a context whose calls carry an identity token for owner.
func (h *harness) ctxFor(t *testing.T, owner string) context.Context {
	t.Helper()
	token, err := h.authority.Sign(authz.Identity{OwnerID: owner, RequestID: "test-request"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return metadata.AppendToOutgoingContext(context.Background(), authz.Header, token)
}

// ctx returns a context for the harness's own owner.
func (h *harness) ctx(t *testing.T) context.Context {
	return h.ctxFor(t, h.owner)
}

// fakeQueue records the generation jobs the use cases ask for. A non-nil err
// makes every enqueue fail.
type fakeQueue struct {
	mu   sync.Mutex
	jobs []app.GenerateArgs
	err  error
}

func (q *fakeQueue) EnqueueGenerate(_ context.Context, _ pgx.Tx, args app.GenerateArgs) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.jobs = append(q.jobs, args)
	return nil
}

func (q *fakeQueue) enqueued() []app.GenerateArgs {
	q.mu.Lock()
	defer q.mu.Unlock()
	return slices.Clone(q.jobs)
}

// requireStatus fails unless err has the gRPC code and ErrorInfo reason.
func requireStatus(t *testing.T, err error, code codes.Code, reason string) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok || st.Code() != code {
		t.Fatalf("got %v, want code %s", err, code)
	}
	if reason == "" {
		return
	}
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok && info.GetReason() == reason {
			return
		}
	}
	t.Fatalf("got %v, want reason %s", err, reason)
}
