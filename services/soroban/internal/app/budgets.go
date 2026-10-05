package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
)

// maxThreshold is the highest percentage a notification threshold may be set
// to; 100 is the budget's limit.
const maxThreshold = 100

// defaultThresholds are the notification points of a budget created without
// any.
var defaultThresholds = []int32{50, 80, 100}

// BudgetInput creates a budget (ID empty) or updates one. On update only the
// limit, mode, thresholds and enabled flag change: scope and period identify
// the budget.
type BudgetInput struct {
	ID          string
	ScopeType   domain.ScopeType
	ScopeValue  string
	Period      domain.Period
	LimitMicros int64
	Mode        domain.Mode
	Thresholds  []int32
	Enabled     bool
	Version     int32
}

// BudgetView is a budget with the figures of its current period.
type BudgetView struct {
	Budget         db.Budget
	SpentMicros    int64
	ReservedMicros int64
	ResetsAt       time.Time
}

// SetBudget creates a budget, or updates one if its version matches.
func (s *Service) SetBudget(ctx context.Context, in BudgetInput) (BudgetView, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return BudgetView{}, err
	}
	if err := validateBudget(in); err != nil {
		return BudgetView{}, err
	}
	if len(in.Thresholds) == 0 {
		in.Thresholds = defaultThresholds
	}

	var budget db.Budget
	err = s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		var err error
		budget, err = saveBudget(ctx, repo, owner, in)
		return err
	})
	if err != nil {
		return BudgetView{}, err
	}
	return s.viewOf(budget, nil), nil
}

func saveBudget(ctx context.Context, repo *store.Repo, owner uuid.UUID, in BudgetInput) (db.Budget, error) {
	if in.ID == "" {
		return repo.InsertBudget(ctx, db.InsertBudgetParams{
			ID: store.NewID(), OwnerID: owner, ScopeType: string(in.ScopeType), ScopeValue: in.ScopeValue,
			Period: string(in.Period), LimitMicros: in.LimitMicros, Mode: string(in.Mode),
			Thresholds: in.Thresholds, Enabled: in.Enabled,
		})
	}
	id, err := uuid.Parse(in.ID)
	if err != nil {
		return db.Budget{}, invalid(ReasonInvalidBudget, "id %q is not a UUID", in.ID)
	}
	return repo.UpdateBudget(ctx, owner, db.UpdateBudgetParams{
		ID: id, OwnerID: owner, Version: in.Version, LimitMicros: in.LimitMicros,
		Mode: string(in.Mode), Thresholds: in.Thresholds, Enabled: in.Enabled,
	})
}

func validateBudget(in BudgetInput) error {
	if !in.ScopeType.Valid() || !in.Period.Valid() || !in.Mode.Valid() {
		return invalid(ReasonInvalidBudget, "scope type, period and mode are required")
	}
	if in.LimitMicros < 0 {
		return invalid(ReasonInvalidBudget, "limit must not be negative")
	}
	switch {
	case in.ScopeType == domain.ScopeGlobal && in.ScopeValue != "":
		return invalid(ReasonInvalidBudget, "a global budget has no scope value")
	case in.ScopeType == domain.ScopeService && (in.ScopeValue == "" || strings.Contains(in.ScopeValue, ".")):
		return invalid(ReasonInvalidBudget, "a service budget's scope value is a service name")
	case in.ScopeType == domain.ScopeFeature && !strings.Contains(in.ScopeValue, "."):
		return invalid(ReasonInvalidBudget, "a feature budget's scope value is service.feature")
	}
	for _, t := range in.Thresholds {
		if t < 1 || t > maxThreshold {
			return invalid(ReasonInvalidBudget, "thresholds are percentages from 1 to %d", maxThreshold)
		}
	}
	return nil
}

// ListBudgets returns the owner's budgets with their current spend, creating
// the defaults the first time.
func (s *Service) ListBudgets(ctx context.Context) ([]BudgetView, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var (
		budgets []db.Budget
		periods []db.BudgetPeriod
	)
	err = s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		if err := repo.EnsureDefaultBudgets(ctx, owner); err != nil {
			return err
		}
		var err error
		if budgets, err = repo.ListBudgets(ctx, owner); err != nil {
			return fmt.Errorf("list budgets: %w", err)
		}
		if periods, err = repo.CurrentPeriods(ctx, owner, now); err != nil {
			return fmt.Errorf("list periods: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	byBudget := make(map[uuid.UUID]db.BudgetPeriod, len(periods))
	for _, p := range periods {
		byBudget[p.BudgetID] = p
	}
	views := make([]BudgetView, 0, len(budgets))
	for _, b := range budgets {
		var current *db.BudgetPeriod
		if p, ok := byBudget[b.ID]; ok {
			current = &p
		}
		views = append(views, s.viewOf(b, current))
	}
	return views, nil
}

// viewOf adds the current period's figures to a budget. A budget with no
// period yet has spent nothing; its reset time comes from the clock.
func (s *Service) viewOf(b db.Budget, current *db.BudgetPeriod) BudgetView {
	if current != nil {
		return BudgetView{Budget: b, SpentMicros: current.SpentMicros, ReservedMicros: current.ReservedMicros, ResetsAt: current.PeriodEnd}
	}
	_, end, err := domain.PeriodBounds(s.now(), domain.Period(b.Period), s.loc)
	if err != nil {
		// The period came from a CHECK-constrained column, so this cannot happen.
		return BudgetView{Budget: b}
	}
	return BudgetView{Budget: b, ResetsAt: end}
}
