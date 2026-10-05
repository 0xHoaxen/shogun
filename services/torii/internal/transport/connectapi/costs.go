package connectapi

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

const (
	reasonInvalidRange = "INVALID_RANGE"
	reasonInvalidGroup = "INVALID_GROUP"

	// maxSpendDays bounds a spend query, which is a year of daily rows at most.
	maxSpendDays = 366

	// istOffset is the offset of Asia/Kolkata, the zone the owner's days and
	// budget periods are counted in. India has no daylight saving time.
	istOffset = 5*time.Hour + 30*time.Minute
)

// CostsBackend is the part of soroban's client CostsServer uses.
type CostsBackend interface {
	GetSpend(ctx context.Context, in *sorobanv1.GetSpendRequest, opts ...grpc.CallOption) (*sorobanv1.GetSpendResponse, error)
	ListBudgets(ctx context.Context, in *sorobanv1.ListBudgetsRequest, opts ...grpc.CallOption) (*sorobanv1.ListBudgetsResponse, error)
	SetBudget(ctx context.Context, in *sorobanv1.SetBudgetRequest, opts ...grpc.CallOption) (*sorobanv1.SetBudgetResponse, error)
}

// CostsServer implements shogun.api.v1.CostsService on top of soroban.
type CostsServer struct {
	soroban CostsBackend
	log     *slog.Logger
}

var _ apiv1connect.CostsServiceHandler = (*CostsServer)(nil)

// NewCostsServer returns a CostsServer that calls soroban through backend.
func NewCostsServer(backend CostsBackend, log *slog.Logger) *CostsServer {
	return &CostsServer{soroban: backend, log: log}
}

// GetSpend totals the spend of a range of days, grouped as asked.
func (s *CostsServer) GetSpend(
	ctx context.Context, req *connect.Request[apiv1.GetSpendRequest],
) (*connect.Response[apiv1.GetSpendResponse], error) {
	in := req.Msg
	from, to, err := dayRange(in.GetFromDay(), in.GetToDay())
	if err != nil {
		return nil, err
	}
	group, ok := spendGroupToSoroban(in.GetGroupBy())
	if !ok {
		return nil, newError(connect.CodeInvalidArgument, reasonInvalidGroup, "group_by is required")
	}
	resp, err := s.soroban.GetSpend(ctx, &sorobanv1.GetSpendRequest{
		From: timestampOf(from), To: timestampOf(to), GroupBy: group,
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(spendToAPI(resp)), nil
}

// ListBudgets returns every budget with the spend of its current period.
func (s *CostsServer) ListBudgets(
	ctx context.Context, _ *connect.Request[apiv1.ListBudgetsRequest],
) (*connect.Response[apiv1.ListBudgetsResponse], error) {
	resp, err := s.soroban.ListBudgets(ctx, &sorobanv1.ListBudgetsRequest{})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	budgets := make([]*apiv1.Budget, 0, len(resp.GetBudgets()))
	for _, b := range resp.GetBudgets() {
		budgets = append(budgets, budgetToAPI(b))
	}
	return connect.NewResponse(&apiv1.ListBudgetsResponse{Budgets: budgets}), nil
}

// UpdateBudget changes a budget's limit, mode, thresholds and enabled flag.
func (s *CostsServer) UpdateBudget(
	ctx context.Context, req *connect.Request[apiv1.UpdateBudgetRequest],
) (*connect.Response[apiv1.UpdateBudgetResponse], error) {
	in := req.Msg
	if in.GetId() == "" {
		return nil, newError(connect.CodeInvalidArgument, reasonInvalidBudget, "id is required")
	}
	resp, err := s.soroban.SetBudget(ctx, &sorobanv1.SetBudgetRequest{
		Id:          in.GetId(),
		Version:     in.GetVersion(),
		LimitMicros: in.GetLimitMicros(),
		Mode:        budgetModeToSoroban(in.GetMode()),
		Thresholds:  in.GetThresholds(),
		Enabled:     in.GetEnabled(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.UpdateBudgetResponse{Budget: budgetToAPI(resp.GetBudget())}), nil
}

// dayRange turns the days fromDay and toDay, both included, into the half-open
// interval of instants [from, to) in Asia/Kolkata.
func dayRange(fromDay, toDay string) (from, to time.Time, err error) {
	loc := time.FixedZone("Asia/Kolkata", int(istOffset/time.Second))
	first, err := time.ParseInLocation(time.DateOnly, fromDay, loc)
	if err != nil {
		return time.Time{}, time.Time{}, newError(connect.CodeInvalidArgument, reasonInvalidRange, "from_day must be YYYY-MM-DD")
	}
	last, err := time.ParseInLocation(time.DateOnly, toDay, loc)
	if err != nil {
		return time.Time{}, time.Time{}, newError(connect.CodeInvalidArgument, reasonInvalidRange, "to_day must be YYYY-MM-DD")
	}
	if last.Before(first) {
		return time.Time{}, time.Time{}, newError(connect.CodeInvalidArgument, reasonInvalidRange, "to_day is before from_day")
	}
	end := last.AddDate(0, 0, 1)
	if end.Sub(first) > maxSpendDays*24*time.Hour {
		return time.Time{}, time.Time{}, newError(connect.CodeInvalidArgument, reasonInvalidRange, "the range is longer than a year")
	}
	return first, end, nil
}
