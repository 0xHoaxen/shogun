package grpc_test

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/anypb"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/events"
	"github.com/0xHoaxen/shogun/services/taiko/internal/live"
	taikogrpc "github.com/0xHoaxen/shogun/services/taiko/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/taiko/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

// harness is a taiko server on an in-memory listener with the real authz
// interceptors, the real live feed and the real event sink, backed by a
// migrated Postgres schema.
type harness struct {
	client    taikov1.TaikoServiceClient
	pool      *pgxpool.Pool
	sink      *bus.SinkServer
	authority *authz.Authority
	owner     string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "taiko")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	log := slog.New(slog.DiscardHandler)
	svc := app.NewService(pool, nil)
	stopLive, err := live.New(pool, svc, log, live.WithRetryDelay(50*time.Millisecond)).Start(ctx)
	if err != nil {
		t.Fatalf("live feed: %v", err)
	}
	t.Cleanup(stopLive)
	sink, err := bus.NewSinkServer(pool, events.Handlers(svc, log), log)
	if err != nil {
		t.Fatalf("sink: %v", err)
	}
	authority, err := authz.New(bytes.Repeat([]byte("k"), authz.MinKeyLength))
	if err != nil {
		t.Fatalf("authority: %v", err)
	}
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(authority.UnaryServerInterceptor()),
		grpc.StreamInterceptor(authority.StreamServerInterceptor()),
	)
	taikov1.RegisterTaikoServiceServer(srv, taikogrpc.New(svc))
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
		client: taikov1.NewTaikoServiceClient(conn), pool: pool, sink: sink, authority: authority, owner: uuid.NewString(),
	}
}

// ctxFor returns a context whose calls carry an identity token for owner.
func (h *harness) ctxFor(t *testing.T, owner string) context.Context {
	t.Helper()
	return h.ctxFrom(context.Background(), t, owner)
}

// ctxFrom is ctxFor on top of parent, for calls that need to be cancelled.
func (h *harness) ctxFrom(parent context.Context, t *testing.T, owner string) context.Context {
	t.Helper()
	token, err := h.authority.Sign(authz.Identity{OwnerID: owner, RequestID: "test-request"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return metadata.AppendToOutgoingContext(parent, authz.Header, token)
}

// ctx returns a context for the harness's own owner.
func (h *harness) ctx(t *testing.T) context.Context {
	return h.ctxFor(t, h.owner)
}

// draftReady delivers a draft.ready event for owner through the real sink, as
// the outbox relay would, and so creates one notification.
func (h *harness) draftReady(t *testing.T, owner string) {
	t.Helper()
	packed, err := anypb.New(&fudev1.DraftReady{
		OwnerId: owner, DraftId: uuid.NewString(), Kind: fudev1.DraftKind_DRAFT_KIND_COVER_LETTER,
	})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	_, err = h.sink.Deliver(context.Background(), &eventsv1.DeliverRequest{Envelope: &eventsv1.Envelope{
		Id: uuid.NewString(), Type: "draft.ready", Source: "fude", Payload: packed,
	}})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
}

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
