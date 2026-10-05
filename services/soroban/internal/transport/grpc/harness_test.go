package grpc_test

import (
	"bytes"
	"context"
	"net"
	"os"
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

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/soroban/internal/app"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
	sorobangrpc "github.com/0xHoaxen/shogun/services/soroban/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/soroban/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

// fixedNow is the clock of every harness: noon UTC on 5 Oct 2026, which is
// 17:30 IST, so the monthly period is October and ends 31 Oct 18:30 UTC.
var fixedNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

// harness is a soroban server on an in-memory listener with the real authz
// interceptors, backed by a migrated Postgres schema.
type harness struct {
	client    sorobanv1.SorobanServiceClient
	pool      *pgxpool.Pool
	authority *authz.Authority
	owner     string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "soroban")
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
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(authority.UnaryServerInterceptor()),
		grpc.StreamInterceptor(authority.StreamServerInterceptor()),
	)
	sorobanv1.RegisterSorobanServiceServer(srv, sorobangrpc.New(app.NewService(pool, func() time.Time { return fixedNow })))
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
		client:    sorobanv1.NewSorobanServiceClient(conn),
		pool:      pool,
		authority: authority,
		owner:     uuid.NewString(),
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

// seedDefaults gives the harness's owner the default budgets.
func (h *harness) seedDefaults(t *testing.T) {
	t.Helper()
	if err := store.New(h.pool).EnsureDefaultBudgets(context.Background(), uuid.MustParse(h.owner)); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
}

// setLimit sets the limit of one of the owner's budgets, by scope.
func (h *harness) setLimit(t *testing.T, scopeType, scopeValue string, limitMicros int64) {
	t.Helper()
	tag, err := h.pool.Exec(context.Background(),
		`UPDATE budgets SET limit_micros = $1 WHERE owner_id = $2 AND scope_type = $3 AND scope_value = $4`,
		limitMicros, h.owner, scopeType, scopeValue)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("set limit: %v (rows %d)", err, tag.RowsAffected())
	}
}

// reserved returns the reserved total of the owner's budget with the scope.
func (h *harness) reserved(t *testing.T, scopeType, scopeValue string) int64 {
	t.Helper()
	var n int64
	err := h.pool.QueryRow(context.Background(),
		`SELECT COALESCE(sum(p.reserved_micros), 0)
		   FROM budget_periods p JOIN budgets b ON b.id = p.budget_id
		  WHERE b.owner_id = $1 AND b.scope_type = $2 AND b.scope_value = $3`,
		h.owner, scopeType, scopeValue).Scan(&n)
	if err != nil {
		t.Fatalf("reserved: %v", err)
	}
	return n
}

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

// errorInfo returns the ErrorInfo detail of err, or nil.
func errorInfo(err error) *errdetails.ErrorInfo {
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	return nil
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
	if info := errorInfo(err); info == nil || info.GetReason() != reason {
		t.Fatalf("got %v, want reason %s", err, reason)
	}
}
