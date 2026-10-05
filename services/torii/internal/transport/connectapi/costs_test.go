package connectapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

// fakeSoroban answers the cost calls from the funcs a test sets, and records
// the owner each call acted for and the requests it received.
type fakeSoroban struct {
	spend  func(*sorobanv1.GetSpendRequest) (*sorobanv1.GetSpendResponse, error)
	list   func(*sorobanv1.ListBudgetsRequest) (*sorobanv1.ListBudgetsResponse, error)
	update func(*sorobanv1.SetBudgetRequest) (*sorobanv1.SetBudgetResponse, error)

	mu       sync.Mutex
	owners   []string
	requests []any
}

func (f *fakeSoroban) record(ctx context.Context, in any) {
	id, _ := authz.FromContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners = append(f.owners, id.OwnerID)
	f.requests = append(f.requests, in)
}

func (f *fakeSoroban) requestsSeen() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.requests...)
}

func (f *fakeSoroban) ownersSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.owners...)
}

func (f *fakeSoroban) GetSpend(ctx context.Context, in *sorobanv1.GetSpendRequest, _ ...grpc.CallOption) (*sorobanv1.GetSpendResponse, error) {
	f.record(ctx, in)
	return f.spend(in)
}

func (f *fakeSoroban) ListBudgets(ctx context.Context, in *sorobanv1.ListBudgetsRequest, _ ...grpc.CallOption) (*sorobanv1.ListBudgetsResponse, error) {
	f.record(ctx, in)
	return f.list(in)
}

func (f *fakeSoroban) SetBudget(ctx context.Context, in *sorobanv1.SetBudgetRequest, _ ...grpc.CallOption) (*sorobanv1.SetBudgetResponse, error) {
	f.record(ctx, in)
	return f.update(in)
}

type costsHarness struct {
	soroban *fakeSoroban
	client  apiv1connect.CostsServiceClient
	token   string
	owner   string
}

func newCostsHarness(t *testing.T) *costsHarness {
	t.Helper()
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	auth, err := app.NewAuth(apptest.NewMemSessions(),
		app.AuthConfig{AllowedEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	token, session, err := auth.StartSession(context.Background(),
		app.Claims{Subject: "sub-1", Email: ownerEmail, EmailVerified: true})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	soroban := &fakeSoroban{}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewCostsServiceHandler(connectapi.NewCostsServer(soroban, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &costsHarness{
		soroban: soroban, token: token, owner: session.OwnerID.String(),
		client: apiv1connect.NewCostsServiceClient(srv.Client(), srv.URL),
	}
}

func sorobanBudget(id string, scope sorobanv1.ScopeType, value string, limit int64) *sorobanv1.Budget {
	return &sorobanv1.Budget{
		Id: id, ScopeType: scope, ScopeValue: value, Period: sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY,
		LimitMicros: limit, Mode: sorobanv1.BudgetMode_BUDGET_MODE_HARD, Thresholds: []int32{50, 80, 100},
		Enabled: true, Version: 4, SpentMicros: 2_000_000, ReservedMicros: 500_000,
		ResetsAt: timestamppb.New(time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC)),
	}
}

func TestGetSpendSendsISTDayBoundsAndMapsRows(t *testing.T) {
	// Arrange
	h := newCostsHarness(t)
	h.soroban.spend = func(*sorobanv1.GetSpendRequest) (*sorobanv1.GetSpendResponse, error) {
		return &sorobanv1.GetSpendResponse{
			Rows: []*sorobanv1.SpendRow{{
				Key: "fude", CostMicros: 6000,
				Usage: &sorobanv1.Usage{InputTokens: 1500, OutputTokens: 20, CacheReadTokens: 7, CacheWriteTokens: 3},
			}},
			TotalMicros: 6000,
		}, nil
	}

	// Act
	resp, err := h.client.GetSpend(context.Background(), withCookie(connect.NewRequest(&apiv1.GetSpendRequest{
		FromDay: "2026-10-01", ToDay: "2026-10-03", GroupBy: apiv1.SpendGroup_SPEND_GROUP_SERVICE,
	}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("GetSpend: %v", err)
	}
	want := &apiv1.SpendRow{Key: "fude", CostMicros: 6000, InputTokens: 1500, OutputTokens: 20, CacheReadTokens: 7, CacheWriteTokens: 3}
	if rows := resp.Msg.GetRows(); len(rows) != 1 || !proto.Equal(rows[0], want) || resp.Msg.GetTotalMicros() != 6000 {
		t.Fatalf("response = %v", resp.Msg)
	}
	sent, ok := h.soroban.requestsSeen()[0].(*sorobanv1.GetSpendRequest)
	if !ok || sent.GetGroupBy() != sorobanv1.SpendGroup_SPEND_GROUP_SERVICE {
		t.Fatalf("soroban request = %v", h.soroban.requestsSeen()[0])
	}
	// 1 Oct 00:00 IST is 30 Sep 18:30 UTC; the range ends before 4 Oct 00:00 IST.
	if got, want := sent.GetFrom().AsTime(), time.Date(2026, 9, 30, 18, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("from = %s, want %s", got, want)
	}
	if got, want := sent.GetTo().AsTime(), time.Date(2026, 10, 3, 18, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("to = %s, want %s", got, want)
	}
	if owners := h.soroban.ownersSeen(); !reflect.DeepEqual(owners, []string{h.owner}) {
		t.Fatalf("soroban call acted for %v, want the signed-in owner", owners)
	}
}

func TestGetSpendMapsEveryGroup(t *testing.T) {
	tests := []struct {
		group apiv1.SpendGroup
		want  sorobanv1.SpendGroup
	}{
		{apiv1.SpendGroup_SPEND_GROUP_DAY, sorobanv1.SpendGroup_SPEND_GROUP_DAY},
		{apiv1.SpendGroup_SPEND_GROUP_SERVICE, sorobanv1.SpendGroup_SPEND_GROUP_SERVICE},
		{apiv1.SpendGroup_SPEND_GROUP_FEATURE, sorobanv1.SpendGroup_SPEND_GROUP_FEATURE},
		{apiv1.SpendGroup_SPEND_GROUP_MODEL, sorobanv1.SpendGroup_SPEND_GROUP_MODEL},
	}
	for _, tt := range tests {
		t.Run(tt.group.String(), func(t *testing.T) {
			h := newCostsHarness(t)
			h.soroban.spend = func(*sorobanv1.GetSpendRequest) (*sorobanv1.GetSpendResponse, error) {
				return &sorobanv1.GetSpendResponse{}, nil
			}

			_, err := h.client.GetSpend(context.Background(), withCookie(connect.NewRequest(&apiv1.GetSpendRequest{
				FromDay: "2026-10-01", ToDay: "2026-10-01", GroupBy: tt.group,
			}), h.token))
			if err != nil {
				t.Fatalf("GetSpend: %v", err)
			}
			if got := h.soroban.requestsSeen()[0].(*sorobanv1.GetSpendRequest).GetGroupBy(); got != tt.want {
				t.Errorf("group = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGetSpendRejectsBadRangesWithoutCallingSoroban(t *testing.T) {
	tests := []struct {
		name       string
		req        *apiv1.GetSpendRequest
		wantReason string
	}{
		{"missing from", &apiv1.GetSpendRequest{ToDay: "2026-10-03", GroupBy: apiv1.SpendGroup_SPEND_GROUP_DAY}, "INVALID_RANGE"},
		{"malformed to", &apiv1.GetSpendRequest{FromDay: "2026-10-01", ToDay: "03/10/2026", GroupBy: apiv1.SpendGroup_SPEND_GROUP_DAY}, "INVALID_RANGE"},
		{"to before from", &apiv1.GetSpendRequest{FromDay: "2026-10-03", ToDay: "2026-10-01", GroupBy: apiv1.SpendGroup_SPEND_GROUP_DAY}, "INVALID_RANGE"},
		{"longer than a year", &apiv1.GetSpendRequest{FromDay: "2025-01-01", ToDay: "2026-10-01", GroupBy: apiv1.SpendGroup_SPEND_GROUP_DAY}, "INVALID_RANGE"},
		{"no group", &apiv1.GetSpendRequest{FromDay: "2026-10-01", ToDay: "2026-10-03"}, "INVALID_GROUP"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newCostsHarness(t)

			_, err := h.client.GetSpend(context.Background(), withCookie(connect.NewRequest(tt.req), h.token))

			if connect.CodeOf(err) != connect.CodeInvalidArgument || reasonOf(err) != tt.wantReason {
				t.Fatalf("code %v reason %q, want InvalidArgument %s", connect.CodeOf(err), reasonOf(err), tt.wantReason)
			}
			if len(h.soroban.requestsSeen()) != 0 {
				t.Fatal("soroban was called for an invalid request")
			}
		})
	}
}

func TestListBudgetsMapsScopePeriodModeAndResetTime(t *testing.T) {
	// Arrange
	h := newCostsHarness(t)
	h.soroban.list = func(*sorobanv1.ListBudgetsRequest) (*sorobanv1.ListBudgetsResponse, error) {
		return &sorobanv1.ListBudgetsResponse{Budgets: []*sorobanv1.Budget{
			sorobanBudget("b1", sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL, "", 20_000_000),
			sorobanBudget("b2", sorobanv1.ScopeType_SCOPE_TYPE_FEATURE, "fude.post", 1_000_000),
		}}, nil
	}

	// Act
	resp, err := h.client.ListBudgets(context.Background(), withCookie(connect.NewRequest(&apiv1.ListBudgetsRequest{}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("ListBudgets: %v", err)
	}
	got := resp.Msg.GetBudgets()
	if len(got) != 2 {
		t.Fatalf("got %d budgets, want 2", len(got))
	}
	first := got[0]
	if first.GetId() != "b1" || first.GetScope() != apiv1.BudgetScope_BUDGET_SCOPE_GLOBAL ||
		first.GetPeriod() != apiv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY || first.GetMode() != apiv1.BudgetMode_BUDGET_MODE_HARD ||
		first.GetLimitMicros() != 20_000_000 || first.GetSpentMicros() != 2_000_000 || first.GetReservedMicros() != 500_000 ||
		first.GetVersion() != 4 || !first.GetEnabled() || len(first.GetThresholds()) != 3 ||
		first.GetResetsAt() != "2026-10-31T18:30:00Z" {
		t.Errorf("first budget = %v", first)
	}
	if got[1].GetScope() != apiv1.BudgetScope_BUDGET_SCOPE_FEATURE || got[1].GetScopeValue() != "fude.post" {
		t.Errorf("second budget = %v", got[1])
	}
}

func TestUpdateBudgetForwardsOnlyTheEditableFields(t *testing.T) {
	// Arrange
	h := newCostsHarness(t)
	h.soroban.update = func(in *sorobanv1.SetBudgetRequest) (*sorobanv1.SetBudgetResponse, error) {
		b := sorobanBudget(in.GetId(), sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL, "", in.GetLimitMicros())
		b.Version = in.GetVersion() + 1
		return &sorobanv1.SetBudgetResponse{Budget: b}, nil
	}

	// Act
	resp, err := h.client.UpdateBudget(context.Background(), withCookie(connect.NewRequest(&apiv1.UpdateBudgetRequest{
		Id: "b1", LimitMicros: 25_000_000, Mode: apiv1.BudgetMode_BUDGET_MODE_SOFT,
		Thresholds: []int32{50, 90}, Enabled: true, Version: 4,
	}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("UpdateBudget: %v", err)
	}
	if b := resp.Msg.GetBudget(); b.GetLimitMicros() != 25_000_000 || b.GetVersion() != 5 {
		t.Fatalf("budget = %v", b)
	}
	sent, ok := h.soroban.requestsSeen()[0].(*sorobanv1.SetBudgetRequest)
	if !ok || sent.GetId() != "b1" || sent.GetVersion() != 4 || sent.GetLimitMicros() != 25_000_000 ||
		sent.GetMode() != sorobanv1.BudgetMode_BUDGET_MODE_SOFT || !sent.GetEnabled() ||
		!reflect.DeepEqual(sent.GetThresholds(), []int32{50, 90}) {
		t.Fatalf("soroban request = %v", h.soroban.requestsSeen()[0])
	}
	if sent.GetScopeType() != sorobanv1.ScopeType_SCOPE_TYPE_UNSPECIFIED || sent.GetScopeValue() != "" {
		t.Errorf("an update must not send a scope: %v", sent)
	}
}

func TestUpdateBudgetRequiresAnID(t *testing.T) {
	h := newCostsHarness(t)

	_, err := h.client.UpdateBudget(context.Background(), withCookie(connect.NewRequest(&apiv1.UpdateBudgetRequest{LimitMicros: 1}), h.token))

	if connect.CodeOf(err) != connect.CodeInvalidArgument || reasonOf(err) != "INVALID_BUDGET" {
		t.Fatalf("code %v reason %q, want InvalidArgument INVALID_BUDGET", connect.CodeOf(err), reasonOf(err))
	}
	if len(h.soroban.requestsSeen()) != 0 {
		t.Fatal("soroban was called without an id")
	}
}

func TestUpdateBudgetKeepsDownstreamReasons(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   connect.Code
		wantReason string
	}{
		{"stale version", grpcError(codes.Aborted, "VERSION_CONFLICT", "changed"), connect.CodeAborted, "VERSION_CONFLICT"},
		{"invalid budget", grpcError(codes.InvalidArgument, "INVALID_BUDGET", "limit must not be negative"), connect.CodeInvalidArgument, "INVALID_BUDGET"},
		{"unknown budget", grpcError(codes.NotFound, "RESOURCE_NOT_FOUND", "not found"), connect.CodeNotFound, "RESOURCE_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newCostsHarness(t)
			h.soroban.update = func(*sorobanv1.SetBudgetRequest) (*sorobanv1.SetBudgetResponse, error) { return nil, tt.err }

			_, err := h.client.UpdateBudget(context.Background(), withCookie(connect.NewRequest(&apiv1.UpdateBudgetRequest{
				Id: "b1", LimitMicros: 1, Mode: apiv1.BudgetMode_BUDGET_MODE_HARD, Version: 1,
			}), h.token))

			if connect.CodeOf(err) != tt.wantCode || reasonOf(err) != tt.wantReason {
				t.Fatalf("code %v reason %q, want %v %s", connect.CodeOf(err), reasonOf(err), tt.wantCode, tt.wantReason)
			}
		})
	}
}

func TestCostsRequireASession(t *testing.T) {
	h := newCostsHarness(t)

	_, err := h.client.ListBudgets(context.Background(), connect.NewRequest(&apiv1.ListBudgetsRequest{}))

	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", connect.CodeOf(err))
	}
	if len(h.soroban.requestsSeen()) != 0 {
		t.Fatal("soroban was called without a session")
	}
}
