package grpc_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/dojo/internal/app"
	dojogrpc "github.com/0xHoaxen/shogun/services/dojo/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/dojo/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

// testNow is the fake clock: 15:30 on 7 October 2026 in Asia/Kolkata.
var testNow = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)

// fakeFude records the draft requests it gets and answers with a fixed draft
// id, or with err.
type fakeFude struct {
	mu      sync.Mutex
	err     error
	draftID string
	calls   []*fudev1.GenerateDraftRequest
}

func (f *fakeFude) GenerateDraft(_ context.Context, in *fudev1.GenerateDraftRequest, _ ...grpc.CallOption) (*fudev1.GenerateDraftResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, in)
	if f.err != nil {
		return nil, f.err
	}
	return &fudev1.GenerateDraftResponse{Draft: &fudev1.Draft{Id: f.draftID}}, nil
}

func (f *fakeFude) requests() []*fudev1.GenerateDraftRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fudev1.GenerateDraftRequest(nil), f.calls...)
}

// harness is a dojo server on an in-memory listener with the real authz
// interceptors, backed by a migrated Postgres schema and a fake fude.
type harness struct {
	client    dojov1.DojoServiceClient
	pool      *pgxpool.Pool
	authority *authz.Authority
	fude      *fakeFude
	owner     string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "dojo")
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
	fude := &fakeFude{draftID: uuid.NewString()}
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(authority.UnaryServerInterceptor()),
		grpc.StreamInterceptor(authority.StreamServerInterceptor()),
	)
	dojov1.RegisterDojoServiceServer(srv, dojogrpc.New(app.NewService(pool, fude, func() time.Time { return testNow })))
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
		client: dojov1.NewDojoServiceClient(conn), pool: pool, authority: authority, fude: fude, owner: uuid.NewString(),
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
func (h *harness) ctx(t *testing.T) context.Context { return h.ctxFor(t, h.owner) }

// outboxCount counts outbox rows of an event type; an empty type counts all.
func (h *harness) outboxCount(t *testing.T, eventType string) int {
	t.Helper()
	var n int
	err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE $1 = '' OR type = $1`, eventType).Scan(&n)
	if err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return n
}

// payloadOf reads the payload of the only outbox event of a type.
func (h *harness) payloadOf(t *testing.T, eventType string, into proto.Message) {
	t.Helper()
	var raw []byte
	if err := h.pool.QueryRow(context.Background(), `SELECT payload FROM outbox WHERE type = $1`, eventType).Scan(&raw); err != nil {
		t.Fatalf("read %s: %v", eventType, err)
	}
	var env eventsv1.Envelope
	if err := proto.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if err := env.GetPayload().UnmarshalTo(into); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
}

func (h *harness) addItem(t *testing.T, title string) *dojov1.Item {
	t.Helper()
	res, err := h.client.AddItem(h.ctx(t), &dojov1.AddItemRequest{
		Title: title, Kind: dojov1.ItemKind_ITEM_KIND_COURSE, Insight: "worth it", Url: "https://example.com/go",
	})
	if err != nil {
		t.Fatalf("add item %q: %v", title, err)
	}
	return res.GetItem()
}

func (h *harness) logActivity(t *testing.T, itemID, summary string) *dojov1.Activity {
	t.Helper()
	res, err := h.client.LogActivity(h.ctx(t), &dojov1.LogActivityRequest{ItemId: itemID, Summary: summary, Minutes: 45})
	if err != nil {
		t.Fatalf("log activity %q: %v", summary, err)
	}
	return res.GetActivity()
}

// requireStatus fails unless err has the gRPC code and ErrorInfo reason.
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

var errFudeDown = errors.New("fude is down")
