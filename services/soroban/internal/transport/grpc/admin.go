package grpc

import (
	"context"
	"math"

	"google.golang.org/protobuf/types/known/timestamppb"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/soroban/internal/app"
	"github.com/0xHoaxen/shogun/services/soroban/internal/wire"
)

// GetSpend implements soroban.v1.SorobanService.
func (s *Server) GetSpend(ctx context.Context, req *sorobanv1.GetSpendRequest) (*sorobanv1.GetSpendResponse, error) {
	groupBy, _ := wire.SpendGroupFromProto(req.GetGroupBy())
	spend, err := s.svc.GetSpend(ctx, app.SpendInput{
		From: req.GetFrom().AsTime(), To: req.GetTo().AsTime(), GroupBy: groupBy,
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return spendToProto(spend), nil
}

// SetBudget implements soroban.v1.SorobanService. With an empty id it creates
// a budget; otherwise it changes the limit, mode, thresholds and enabled flag
// of the budget if version matches.
func (s *Server) SetBudget(ctx context.Context, req *sorobanv1.SetBudgetRequest) (*sorobanv1.SetBudgetResponse, error) {
	scope, _ := wire.ScopeTypeFromProto(req.GetScopeType())
	period, _ := wire.PeriodFromProto(req.GetPeriod())
	mode, _ := wire.ModeFromProto(req.GetMode())
	view, err := s.svc.SetBudget(ctx, app.BudgetInput{
		ID: req.GetId(), ScopeType: scope, ScopeValue: req.GetScopeValue(), Period: period,
		LimitMicros: req.GetLimitMicros(), Mode: mode, Thresholds: req.GetThresholds(),
		Enabled: req.GetEnabled(), Version: req.GetVersion(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &sorobanv1.SetBudgetResponse{Budget: budgetToProto(view)}, nil
}

// ListBudgets implements soroban.v1.SorobanService.
func (s *Server) ListBudgets(ctx context.Context, _ *sorobanv1.ListBudgetsRequest) (*sorobanv1.ListBudgetsResponse, error) {
	views, err := s.svc.ListBudgets(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*sorobanv1.Budget, 0, len(views))
	for _, v := range views {
		out = append(out, budgetToProto(v))
	}
	return &sorobanv1.ListBudgetsResponse{Budgets: out}, nil
}

// SetPrice implements soroban.v1.SorobanService.
func (s *Server) SetPrice(ctx context.Context, req *sorobanv1.SetPriceRequest) (*sorobanv1.SetPriceResponse, error) {
	p := req.GetPrice()
	price, err := s.svc.SetPrice(ctx, app.PriceInput{
		Model: p.GetModel(), EffectiveFrom: p.GetEffectiveFrom(),
		InputMicrosPerMtok: p.GetInputMicrosPerMtok(), OutputMicrosPerMtok: p.GetOutputMicrosPerMtok(),
		CacheReadMicrosPerMtok: p.GetCacheReadMicrosPerMtok(), CacheWriteMicrosPerMtok: p.GetCacheWriteMicrosPerMtok(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &sorobanv1.SetPriceResponse{Price: priceToProto(price)}, nil
}

// ListPrices implements soroban.v1.SorobanService.
func (s *Server) ListPrices(ctx context.Context, _ *sorobanv1.ListPricesRequest) (*sorobanv1.ListPricesResponse, error) {
	prices, err := s.svc.ListPrices(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*sorobanv1.Price, 0, len(prices))
	for _, p := range prices {
		out = append(out, priceToProto(p))
	}
	return &sorobanv1.ListPricesResponse{Prices: out}, nil
}

func spendToProto(spend app.Spend) *sorobanv1.GetSpendResponse {
	rows := make([]*sorobanv1.SpendRow, 0, len(spend.Rows))
	for _, r := range spend.Rows {
		rows = append(rows, &sorobanv1.SpendRow{
			Key:        r.Key,
			CostMicros: r.CostMicros,
			Usage: &sorobanv1.Usage{
				InputTokens:      clampInt32(r.InputTokens),
				OutputTokens:     clampInt32(r.OutputTokens),
				CacheReadTokens:  clampInt32(r.CacheReadTokens),
				CacheWriteTokens: clampInt32(r.CacheWriteTokens),
			},
		})
	}
	return &sorobanv1.GetSpendResponse{Rows: rows, TotalMicros: spend.TotalMicros}
}

func budgetToProto(v app.BudgetView) *sorobanv1.Budget {
	b := v.Budget
	return &sorobanv1.Budget{
		Id:             b.ID.String(),
		ScopeType:      wire.ScopeTypeToProto(domainScope(b.ScopeType)),
		ScopeValue:     b.ScopeValue,
		Period:         wire.PeriodToProto(domainPeriod(b.Period)),
		LimitMicros:    b.LimitMicros,
		Mode:           wire.ModeToProto(domainMode(b.Mode)),
		Thresholds:     b.Thresholds,
		Enabled:        b.Enabled,
		Version:        b.Version,
		SpentMicros:    v.SpentMicros,
		ReservedMicros: v.ReservedMicros,
		ResetsAt:       timestamppb.New(v.ResetsAt),
	}
}

// clampInt32 narrows a token total to the wire's int32, saturating instead of
// wrapping.
func clampInt32(n int64) int32 {
	switch {
	case n > math.MaxInt32:
		return math.MaxInt32
	case n < math.MinInt32:
		return math.MinInt32
	default:
		return int32(n)
	}
}
