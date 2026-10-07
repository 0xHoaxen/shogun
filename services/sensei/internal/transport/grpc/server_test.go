package grpc_test

import (
	"bytes"
	"context"
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

	senseiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/sensei/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/sensei/internal/app"
	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store"
	senseigrpc "github.com/0xHoaxen/shogun/services/sensei/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/sensei/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

var testNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

type harness struct {
	client    senseiv1.SenseiServiceClient
	svc       *app.Service
	authority *authz.Authority
	owner     string
	seed      func(owner, typ string, dim map[string]string, at time.Time)
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "sensei")
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
	svc := app.NewService(pool, func() time.Time { return testNow })
	srv := grpc.NewServer(grpc.UnaryInterceptor(authority.UnaryServerInterceptor()))
	senseiv1.RegisterSenseiServiceServer(srv, senseigrpc.New(svc))
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
	return &harness{
		client: senseiv1.NewSenseiServiceClient(conn), svc: svc, authority: authority, owner: uuid.NewString(),
		seed: func(owner, typ string, dim map[string]string, at time.Time) {
			if _, err := store.New(pool).InsertFact(ctx, domain.Fact{EventID: store.NewID(), OwnerID: uuid.MustParse(owner), Type: typ, Dimension: dim, OccurredAt: at}); err != nil {
				t.Fatalf("seed: %v", err)
			}
		},
	}
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

func at(d int) time.Time { return time.Date(2026, 10, d, 6, 0, 0, 0, time.UTC) }

func TestGetFunnelReturnsRowsTotalsAndRates(t *testing.T) {
	h := newHarness(t)
	h.seed(h.owner, "job.added", map[string]string{"source": "linkedin", "job_id": "j"}, at(1))
	h.seed(h.owner, "job.status_changed", map[string]string{"job_id": "j", "to": "applied"}, at(2))
	h.seed(h.owner, "job.status_changed", map[string]string{"job_id": "j", "to": "interview"}, at(3))
	h.seed(uuid.NewString(), "job.added", map[string]string{"source": "other", "job_id": "z"}, at(1))
	if err := h.svc.Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}

	res, err := h.client.GetFunnel(h.ctx(t), &senseiv1.GetFunnelRequest{From: "2026-10-01", To: "2026-10-07", GroupBy: senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_SOURCE})

	if err != nil || res.GetFrom() != "2026-10-01" || res.GetTo() != "2026-10-07" || len(res.GetRows()) != 1 {
		t.Fatalf("res %v, err %v", res, err)
	}
	row := res.GetRows()[0]
	if row.GetKey() != "linkedin" || row.GetJobsAdded() != 1 || row.GetApplications() != 1 || row.GetInterviews() != 1 || row.GetInterviewRate() != 1 ||
		res.GetTotal().GetKey() != "total" || res.GetTotal().GetApplications() != 1 {
		t.Fatalf("row %+v, total %+v", row, res.GetTotal())
	}
}

func TestGetOutreachStatsReturnsSentRepliesAndTheRate(t *testing.T) {
	h := newHarness(t)
	for d := 1; d <= 4; d++ {
		h.seed(h.owner, "contact.status_changed", map[string]string{"to": "reached_out", "channel": "email"}, at(d))
	}
	h.seed(h.owner, "contact.status_changed", map[string]string{"to": "replied", "channel": "email"}, at(5))
	if err := h.svc.Rollup(context.Background()); err != nil {
		t.Fatal(err)
	}

	byChannel, err := h.client.GetOutreachStats(h.ctx(t), &senseiv1.GetOutreachStatsRequest{From: "2026-10-01", To: "2026-10-07", GroupBy: senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_CHANNEL})
	byStatus, _ := h.client.GetOutreachStats(h.ctx(t), &senseiv1.GetOutreachStatsRequest{From: "2026-10-01", To: "2026-10-07", GroupBy: senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_STATUS})

	if err != nil || len(byChannel.GetRows()) != 1 || byChannel.GetRows()[0].GetSent() != 4 || byChannel.GetRows()[0].GetReplied() != 1 || byChannel.GetRows()[0].GetReplyRate() != 0.25 {
		t.Fatalf("by channel = %v, %v", byChannel, err)
	}
	if len(byStatus.GetRows()) != 2 || byStatus.GetRows()[0].GetKey() != "reached_out" || byStatus.GetRows()[0].GetMovedIn() != 4 || byStatus.GetTotal().GetSent() != 4 {
		t.Fatalf("by status = %v", byStatus)
	}
}

func TestStatsRefuseBadRequestsWithStableReasons(t *testing.T) {
	h := newHarness(t)

	_, noGroup := h.client.GetFunnel(h.ctx(t), &senseiv1.GetFunnelRequest{})
	_, noOutreachGroup := h.client.GetOutreachStats(h.ctx(t), &senseiv1.GetOutreachStatsRequest{})
	_, badRange := h.client.GetFunnel(h.ctx(t), &senseiv1.GetFunnelRequest{From: "2026-10-09", To: "2026-10-01", GroupBy: senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_MONTH})
	_, notADate := h.client.GetOutreachStats(h.ctx(t), &senseiv1.GetOutreachStatsRequest{From: "soon", GroupBy: senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_CHANNEL})

	requireStatus(t, noGroup, codes.InvalidArgument, "INVALID_GROUP_BY")
	requireStatus(t, noOutreachGroup, codes.InvalidArgument, "INVALID_GROUP_BY")
	requireStatus(t, badRange, codes.InvalidArgument, "INVALID_RANGE")
	requireStatus(t, notADate, codes.InvalidArgument, "INVALID_RANGE")
}

func TestStatsRequireAnIdentity(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.GetFunnel(context.Background(), &senseiv1.GetFunnelRequest{GroupBy: senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_SOURCE})

	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("code = %s, want Unauthenticated", got)
	}
}
