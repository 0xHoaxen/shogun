package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
)

// ListBudgets returns the owner's budgets.
func (r *Repo) ListBudgets(ctx context.Context, owner uuid.UUID) ([]db.Budget, error) {
	return r.q.ListBudgets(ctx, owner)
}

// CurrentPeriods returns the owner's budget periods that contain now.
func (r *Repo) CurrentPeriods(ctx context.Context, owner uuid.UUID, now time.Time) ([]db.BudgetPeriod, error) {
	return r.q.ListCurrentPeriods(ctx, db.ListCurrentPeriodsParams{OwnerID: owner, Now: now})
}

// InsertBudget creates a budget; an owner's second budget with the same scope
// and period is ErrDuplicate.
func (r *Repo) InsertBudget(ctx context.Context, p db.InsertBudgetParams) (db.Budget, error) {
	b, err := r.q.InsertBudget(ctx, p)
	return b, mapErr(err)
}

// UpdateBudget changes a budget's limit, mode, thresholds and enabled flag if
// version is the stored one: otherwise ErrVersionConflict, or ErrNotFound when
// the budget does not exist.
func (r *Repo) UpdateBudget(ctx context.Context, owner uuid.UUID, p db.UpdateBudgetParams) (db.Budget, error) {
	b, err := r.q.UpdateBudget(ctx, p)
	if !errors.Is(err, pgx.ErrNoRows) {
		return b, mapErr(err)
	}
	if _, lookupErr := r.q.GetBudget(ctx, db.GetBudgetParams{ID: p.ID, OwnerID: owner}); lookupErr != nil {
		return db.Budget{}, mapErr(lookupErr)
	}
	return db.Budget{}, ErrVersionConflict
}

// UpsertPrice stores a price, replacing one with the same model and date.
func (r *Repo) UpsertPrice(ctx context.Context, p db.UpsertPriceParams) (db.Price, error) {
	return r.q.UpsertPrice(ctx, p)
}

// ListPrices returns every price, newest first within a model.
func (r *Repo) ListPrices(ctx context.Context) ([]db.Price, error) {
	return r.q.ListPrices(ctx)
}

// SumSpend totals the owner's ledger over [from, to) grouped by service,
// feature, model or day.
func (r *Repo) SumSpend(ctx context.Context, owner uuid.UUID, from, to time.Time, groupBy string) ([]db.SumSpendRow, error) {
	rows, err := r.q.SumSpend(ctx, db.SumSpendParams{OwnerID: owner, FromTime: from, ToTime: to, GroupBy: groupBy})
	if err != nil {
		return nil, fmt.Errorf("sum spend: %w", err)
	}
	return rows, nil
}
