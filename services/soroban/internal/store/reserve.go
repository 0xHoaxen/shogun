package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
)

// TemplateOwnerID is the owner the migration seeds the default budgets under.
var TemplateOwnerID = uuid.Nil

// EnsureDefaultBudgets copies the template budgets to owner the first time the
// owner has none. Concurrent first calls are safe: a copy that loses the race
// is ignored by the unique key.
func (r *Repo) EnsureDefaultBudgets(ctx context.Context, owner uuid.UUID) error {
	has, err := r.q.HasBudgets(ctx, owner)
	if err != nil {
		return fmt.Errorf("check budgets: %w", err)
	}
	if has {
		return nil
	}
	templates, err := r.q.ListBudgetTemplates(ctx, TemplateOwnerID)
	if err != nil {
		return fmt.Errorf("list budget templates: %w", err)
	}
	for _, t := range templates {
		err := r.q.InsertBudgetIfAbsent(ctx, db.InsertBudgetIfAbsentParams{
			ID: NewID(), OwnerID: owner, ScopeType: t.ScopeType, ScopeValue: t.ScopeValue,
			Period: t.Period, LimitMicros: t.LimitMicros, Mode: t.Mode,
			Thresholds: t.Thresholds, Enabled: t.Enabled,
		})
		if err != nil {
			return fmt.Errorf("copy budget %s %s: %w", t.ScopeType, t.ScopeValue, err)
		}
	}
	return nil
}

// MatchingBudgets returns the enabled budgets a call by service and feature is
// counted against, ordered by id.
func (r *Repo) MatchingBudgets(ctx context.Context, owner uuid.UUID, service, feature string) ([]db.Budget, error) {
	return r.q.ListMatchingBudgets(ctx, db.ListMatchingBudgetsParams{OwnerID: owner, Service: service, Feature: feature})
}

// LockPeriod creates the period of budgetID that starts at start if it does
// not exist, then returns it locked for update until the transaction ends.
func (r *Repo) LockPeriod(ctx context.Context, budgetID uuid.UUID, start, end time.Time) (db.BudgetPeriod, error) {
	err := r.q.InsertPeriodIfAbsent(ctx, db.InsertPeriodIfAbsentParams{
		ID: NewID(), BudgetID: budgetID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		return db.BudgetPeriod{}, fmt.Errorf("create period: %w", err)
	}
	p, err := r.q.LockPeriod(ctx, db.LockPeriodParams{BudgetID: budgetID, PeriodStart: start})
	if err != nil {
		return db.BudgetPeriod{}, fmt.Errorf("lock period: %w", mapErr(err))
	}
	return p, nil
}

// AddReserved adds delta, which may be negative, to a period's reserved total.
func (r *Repo) AddReserved(ctx context.Context, periodID uuid.UUID, delta int64) error {
	return r.q.AddReserved(ctx, db.AddReservedParams{ID: periodID, Delta: delta})
}

// MarkThresholdNotified records threshold on the period and reports whether it
// was new, so a notification is sent once per period.
func (r *Repo) MarkThresholdNotified(ctx context.Context, p db.BudgetPeriod, threshold int32) (bool, error) {
	for _, n := range p.NotifiedThresholds {
		if n == threshold {
			return false, nil
		}
	}
	if err := r.q.MarkThresholdNotified(ctx, db.MarkThresholdNotifiedParams{ID: p.ID, Threshold: threshold}); err != nil {
		return false, err
	}
	return true, nil
}

// InsertReservation stores an open reservation.
func (r *Repo) InsertReservation(ctx context.Context, p db.InsertReservationParams) (db.Reservation, error) {
	return r.q.InsertReservation(ctx, p)
}

// PriceOn returns the price of model in force on date.
func (r *Repo) PriceOn(ctx context.Context, model string, date time.Time) (db.Price, error) {
	p, err := r.q.GetPriceOn(ctx, db.GetPriceOnParams{Model: model, OnDate: date})
	return p, mapErr(err)
}
