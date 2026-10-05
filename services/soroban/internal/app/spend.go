package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
)

// SpendInput is a range of the ledger and how to group it.
type SpendInput struct {
	From    time.Time
	To      time.Time
	GroupBy string // service, feature, model or day
}

// Spend is the ledger totals over a range.
type Spend struct {
	Rows        []db.SumSpendRow
	TotalMicros int64
}

// GetSpend totals the owner's ledger over [From, To).
func (s *Service) GetSpend(ctx context.Context, in SpendInput) (Spend, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return Spend{}, err
	}
	if in.GroupBy == "" || in.From.IsZero() || in.To.IsZero() || !in.From.Before(in.To) {
		return Spend{}, invalid(ReasonInvalidRange, "from must be before to, and group_by is required")
	}
	var rows []db.SumSpendRow
	err = s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		var err error
		rows, err = repo.SumSpend(ctx, owner, in.From, in.To, in.GroupBy)
		return err
	})
	if err != nil {
		return Spend{}, err
	}
	total := int64(0)
	for _, r := range rows {
		total += r.CostMicros
	}
	return Spend{Rows: rows, TotalMicros: total}, nil
}
