package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
	"github.com/0xHoaxen/shogun/services/soroban/internal/wire"
)

const (
	// reservationTTL is how long an uncommitted reservation holds its amount
	// before the sweeper gives it back.
	reservationTTL = 10 * time.Minute

	// exhaustedMarker is stored in a period's notified_thresholds once its
	// cost.budget_exhausted event is out. Real thresholds are percentages, so
	// a negative value cannot clash with one.
	exhaustedMarker int32 = -1
)

// ReserveInput is a request to hold an estimated cost.
type ReserveInput struct {
	Service         string
	Feature         string
	Model           string
	EstInputTokens  int32
	EstOutputTokens int32
}

// ReserveResult is a held reservation.
type ReserveResult struct {
	ReservationID uuid.UUID
	ExpiresAt     time.Time
	EstMicros     int64
}

// lockedBudget is a budget with its current period, locked for update.
type lockedBudget struct {
	budget db.Budget
	period db.BudgetPeriod
}

// Reserve holds the estimated cost against every budget in scope. A hard
// budget that the estimate would push past its limit refuses the call with a
// *domain.BudgetExhaustedError, and nothing is held.
func (s *Service) Reserve(ctx context.Context, in ReserveInput) (ReserveResult, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ReserveResult{}, err
	}
	if err := validateReserve(in); err != nil {
		return ReserveResult{}, err
	}
	now := s.now()

	var (
		result ReserveResult
		denial *domain.BudgetExhaustedError
	)
	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		est, err := priceUsage(ctx, repo, in.Model, domain.Usage{
			InputTokens: in.EstInputTokens, OutputTokens: in.EstOutputTokens,
		}, now)
		if err != nil {
			return err
		}
		if err := repo.EnsureDefaultBudgets(ctx, owner); err != nil {
			return err
		}
		locked, err := s.lockBudgets(ctx, repo, owner, in, now)
		if err != nil {
			return err
		}
		if blocked := firstHardBlock(locked, est); blocked != nil {
			// Not an error to the transaction: the exhausted event must commit.
			denial, err = s.refuse(ctx, tx, repo, *blocked)
			return err
		}
		result, err = hold(ctx, repo, owner, in, locked, est, now)
		return err
	})
	if err != nil {
		return ReserveResult{}, err
	}
	if denial != nil {
		return ReserveResult{}, denial
	}
	return result, nil
}

func validateReserve(in ReserveInput) error {
	if in.Service == "" || in.Feature == "" || in.Model == "" {
		return invalid(ReasonInvalidRequest, "service, feature and model are required")
	}
	if !strings.HasPrefix(in.Feature, in.Service+".") {
		return invalid(ReasonFeatureMismatch, "feature %q does not belong to service %q", in.Feature, in.Service)
	}
	if in.EstInputTokens < 0 || in.EstOutputTokens < 0 {
		return invalid(ReasonInvalidTokens, "token estimates must not be negative")
	}
	return nil
}

// priceUsage prices usage of model at the price in force at now.
func priceUsage(ctx context.Context, repo *store.Repo, model string, usage domain.Usage, now time.Time) (int64, error) {
	y, m, d := now.UTC().Date()
	price, err := repo.PriceOn(ctx, model, time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
	if errors.Is(err, store.ErrNotFound) {
		return 0, invalid(ReasonModelUnpriced, "no price for model %q", model)
	}
	if err != nil {
		return 0, fmt.Errorf("get price: %w", err)
	}
	cost, err := domain.Cost(domain.Price{
		InputMicrosPerMtok:      price.InputMicrosPerMtok,
		OutputMicrosPerMtok:     price.OutputMicrosPerMtok,
		CacheReadMicrosPerMtok:  price.CacheReadMicrosPerMtok,
		CacheWriteMicrosPerMtok: price.CacheWriteMicrosPerMtok,
	}, usage)
	if err != nil {
		return 0, invalid(ReasonInvalidTokens, "%v", err)
	}
	return cost, nil
}

// lockBudgets locks the current period of every budget in scope. Budgets come
// ordered by id, so concurrent reservations lock in the same order.
func (s *Service) lockBudgets(ctx context.Context, repo *store.Repo, owner uuid.UUID, in ReserveInput, now time.Time) ([]lockedBudget, error) {
	budgets, err := repo.MatchingBudgets(ctx, owner, in.Service, in.Feature)
	if err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	locked := make([]lockedBudget, 0, len(budgets))
	for _, b := range budgets {
		start, end, err := domain.PeriodBounds(now, domain.Period(b.Period), s.loc)
		if err != nil {
			return nil, err
		}
		period, err := repo.LockPeriod(ctx, b.ID, start, end)
		if err != nil {
			return nil, err
		}
		locked = append(locked, lockedBudget{budget: b, period: period})
	}
	return locked, nil
}

// firstHardBlock returns the first hard budget est would push past its limit.
func firstHardBlock(locked []lockedBudget, est int64) *lockedBudget {
	for i := range locked {
		l := locked[i]
		if domain.Mode(l.budget.Mode) != domain.ModeHard {
			continue
		}
		if l.period.SpentMicros+l.period.ReservedMicros+est > l.budget.LimitMicros {
			return &l
		}
	}
	return nil
}

// refuse builds the refusal for a blocked budget and, once per period, writes
// the cost.budget_exhausted event.
func (s *Service) refuse(ctx context.Context, tx pgx.Tx, repo *store.Repo, l lockedBudget) (*domain.BudgetExhaustedError, error) {
	denial := &domain.BudgetExhaustedError{
		BudgetID:   l.budget.ID,
		ScopeType:  domain.ScopeType(l.budget.ScopeType),
		ScopeValue: l.budget.ScopeValue,
		Period:     domain.Period(l.budget.Period),
		ResetsAt:   l.period.PeriodEnd,
	}
	isNew, err := repo.MarkThresholdNotified(ctx, l.period, exhaustedMarker)
	if err != nil {
		return nil, fmt.Errorf("mark exhausted: %w", err)
	}
	if !isNew {
		return denial, nil
	}
	_, err = outbox.Write(ctx, tx, eventSource, eventBudgetExhausted, l.budget.ID.String(), &sorobanv1.CostBudgetExhausted{
		BudgetId:   l.budget.ID.String(),
		ScopeType:  wire.ScopeTypeToProto(denial.ScopeType),
		ScopeValue: denial.ScopeValue,
		Period:     wire.PeriodToProto(denial.Period),
		ResetsAt:   timestamppb.New(denial.ResetsAt),
	})
	if err != nil {
		return nil, fmt.Errorf("write %s event: %w", eventBudgetExhausted, err)
	}
	return denial, nil
}

// hold adds est to every locked period and records the reservation.
func hold(ctx context.Context, repo *store.Repo, owner uuid.UUID, in ReserveInput, locked []lockedBudget, est int64, now time.Time) (ReserveResult, error) {
	periodIDs := make([]uuid.UUID, 0, len(locked))
	for _, l := range locked {
		if err := repo.AddReserved(ctx, l.period.ID, est); err != nil {
			return ReserveResult{}, fmt.Errorf("add reserved: %w", err)
		}
		periodIDs = append(periodIDs, l.period.ID)
	}
	res, err := repo.InsertReservation(ctx, db.InsertReservationParams{
		ID: store.NewID(), OwnerID: owner, Service: in.Service, Feature: in.Feature, Model: in.Model,
		EstMicros: est, BudgetPeriodIds: periodIDs, CreatedAt: now, ExpiresAt: now.Add(reservationTTL),
	})
	if err != nil {
		return ReserveResult{}, fmt.Errorf("insert reservation: %w", err)
	}
	return ReserveResult{ReservationID: res.ID, ExpiresAt: res.ExpiresAt, EstMicros: est}, nil
}
