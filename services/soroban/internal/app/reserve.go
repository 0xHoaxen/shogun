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
	EstInputTokens  int32
	EstOutputTokens int32
}

// ReserveResult is a held reservation.
type ReserveResult struct {
	ReservationID uuid.UUID
	ExpiresAt     time.Time
	EstMicros     int64
	// Provider is the caller's own AI provider, with its key opened, to make the
	// call with.
	Provider ProviderSecret
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
		// The provider comes first: a user with none has no AI features, and
		// nothing may be written for them.
		provider, err := s.providerSecretFor(ctx, repo, owner)
		if err != nil {
			return err
		}
		est, err := priceUsage(ctx, repo, owner, provider.Model, domain.Usage{
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
		result, err = hold(ctx, repo, owner, in, provider.Model, locked, est, now)
		result.Provider = provider
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
	if in.Service == "" || in.Feature == "" {
		return invalid(ReasonInvalidRequest, "service and feature are required")
	}
	if !strings.HasPrefix(in.Feature, in.Service+".") {
		return invalid(ReasonFeatureMismatch, "feature %q does not belong to service %q", in.Feature, in.Service)
	}
	if in.EstInputTokens < 0 || in.EstOutputTokens < 0 {
		return invalid(ReasonInvalidTokens, "token estimates must not be negative")
	}
	return nil
}

// priceUsage prices usage of model for owner at the price in force at now. A
// model nobody has a price for costs 0: its tokens are still metered and
// recorded, it just has no money to count.
func priceUsage(ctx context.Context, repo *store.Repo, owner uuid.UUID, model string, usage domain.Usage, now time.Time) (int64, error) {
	price, priced, err := resolvePrice(ctx, repo, owner, model, now)
	if err != nil {
		return 0, err
	}
	if !priced {
		price = domain.Price{}
	}
	cost, err := domain.Cost(price, usage)
	if err != nil {
		return 0, invalid(ReasonInvalidTokens, "%v", err)
	}
	return cost, nil
}

// resolvePrice finds the price of model for owner: the one the owner entered
// with their provider if it is for this model, otherwise the shared price table.
// priced is false when there is neither.
func resolvePrice(ctx context.Context, repo *store.Repo, owner uuid.UUID, model string, now time.Time) (price domain.Price, priced bool, err error) {
	provider, err := repo.GetProvider(ctx, owner)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return domain.Price{}, false, fmt.Errorf("get provider: %w", err)
	}
	if err == nil && provider.Model == model && provider.InputMicrosPerMtok != nil && provider.OutputMicrosPerMtok != nil {
		in, out := *provider.InputMicrosPerMtok, *provider.OutputMicrosPerMtok
		// A third-party provider publishes no cache prices; cache tokens count as input.
		return domain.Price{
			InputMicrosPerMtok: in, OutputMicrosPerMtok: out, CacheReadMicrosPerMtok: in, CacheWriteMicrosPerMtok: in,
		}, true, nil
	}
	y, m, d := now.UTC().Date()
	row, err := repo.PriceOn(ctx, model, time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
	if errors.Is(err, store.ErrNotFound) {
		return domain.Price{}, false, nil
	}
	if err != nil {
		return domain.Price{}, false, fmt.Errorf("get price: %w", err)
	}
	return domain.Price{
		InputMicrosPerMtok: row.InputMicrosPerMtok, OutputMicrosPerMtok: row.OutputMicrosPerMtok,
		CacheReadMicrosPerMtok: row.CacheReadMicrosPerMtok, CacheWriteMicrosPerMtok: row.CacheWriteMicrosPerMtok,
	}, true, nil
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
		OwnerId:    l.budget.OwnerID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("write %s event: %w", eventBudgetExhausted, err)
	}
	return denial, nil
}

// hold adds est to every locked period and records the reservation.
func hold(ctx context.Context, repo *store.Repo, owner uuid.UUID, in ReserveInput, model string, locked []lockedBudget, est int64, now time.Time) (ReserveResult, error) {
	periodIDs := make([]uuid.UUID, 0, len(locked))
	for _, l := range locked {
		if err := repo.AddReserved(ctx, l.period.ID, est); err != nil {
			return ReserveResult{}, fmt.Errorf("add reserved: %w", err)
		}
		periodIDs = append(periodIDs, l.period.ID)
	}
	res, err := repo.InsertReservation(ctx, db.InsertReservationParams{
		ID: store.NewID(), OwnerID: owner, Service: in.Service, Feature: in.Feature, Model: model,
		EstMicros: est, BudgetPeriodIds: periodIDs, CreatedAt: now, ExpiresAt: now.Add(reservationTTL),
	})
	if err != nil {
		return ReserveResult{}, fmt.Errorf("insert reservation: %w", err)
	}
	return ReserveResult{ReservationID: res.ID, ExpiresAt: res.ExpiresAt, EstMicros: est}, nil
}
