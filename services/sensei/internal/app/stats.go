package app

import (
	"context"
	"time"

	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store"
)

// FunnelResult is a funnel over a range of days.
type FunnelResult struct {
	From, To time.Time
	Rows     []domain.FunnelRow
	Total    domain.FunnelRow
}

// GetFunnel counts the calling owner's jobs at each stage between two days,
// both included, grouped by source or month. Empty bounds mean the last 30
// days.
func (s *Service) GetFunnel(ctx context.Context, from, to string, by domain.FunnelGroup) (FunnelResult, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return FunnelResult{}, err
	}
	start, end, err := domain.DayRange(from, to, s.today())
	if err != nil {
		return FunnelResult{}, err
	}
	rows, err := store.New(s.pool).ListRollups(ctx, owner, start, end, domain.FunnelMetrics)
	if err != nil {
		return FunnelResult{}, err
	}
	grouped, total := domain.Funnel(rows, by)
	return FunnelResult{From: start, To: end, Rows: grouped, Total: total}, nil
}

// OutreachResult is outreach stats over a range of days.
type OutreachResult struct {
	From, To time.Time
	Rows     []domain.OutreachRow
	Total    domain.OutreachRow
}

// GetOutreachStats counts the calling owner's outreach between two days, both
// included, grouped by channel or by the status contacts moved into.
func (s *Service) GetOutreachStats(ctx context.Context, from, to string, by domain.OutreachGroup) (OutreachResult, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return OutreachResult{}, err
	}
	start, end, err := domain.DayRange(from, to, s.today())
	if err != nil {
		return OutreachResult{}, err
	}
	rows, err := store.New(s.pool).ListRollups(ctx, owner, start, end, domain.OutreachMetrics)
	if err != nil {
		return OutreachResult{}, err
	}
	grouped, total := domain.Outreach(rows, by)
	return OutreachResult{From: start, To: end, Rows: grouped, Total: total}, nil
}
