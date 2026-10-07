package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
	"github.com/0xHoaxen/shogun/services/soroban/internal/wire"
)

// expireBatchSize is how many expired reservations one sweep looks at before
// it looks again.
const expireBatchSize = 100

// CommitInput is the real usage of a reserved call.
type CommitInput struct {
	ReservationID string
	Usage         domain.Usage
	RequestID     string
}

// Commit prices the real usage, writes a ledger row and moves the amount from
// reserved to spent on every budget the reservation was held against. Committing
// again returns the same ledger row. A reservation that expired before the
// commit still has its spend recorded, since the call ran.
func (s *Service) Commit(ctx context.Context, in CommitInput) (db.Ledger, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Ledger{}, err
	}
	id, err := parseReservationID(in.ReservationID)
	if err != nil {
		return db.Ledger{}, err
	}
	now := s.now()

	var entry db.Ledger
	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		res, err := repo.LockReservation(ctx, owner, id)
		if err != nil {
			return err
		}
		status := domain.ReservationStatus(res.Status)
		if status == domain.ReservationCommitted {
			entry, err = repo.LedgerEntryByReservation(ctx, id)
			return err
		}
		if !status.CanCommit() {
			return ErrReservationNotOpen
		}
		cost, err := priceUsage(ctx, repo, res.Model, in.Usage, now)
		if err != nil {
			return err
		}
		entry, err = repo.InsertLedgerEntry(ctx, db.InsertLedgerEntryParams{
			ID: store.NewID(), OwnerID: owner, ReservationID: id,
			Service: res.Service, Feature: res.Feature, Model: res.Model,
			InputTokens: in.Usage.InputTokens, OutputTokens: in.Usage.OutputTokens,
			CacheReadTokens: in.Usage.CacheReadTokens, CacheWriteTokens: in.Usage.CacheWriteTokens,
			CostMicros: cost, RequestID: optional(in.RequestID), OccurredAt: now,
		})
		if err != nil {
			return fmt.Errorf("insert ledger entry: %w", err)
		}
		// The hold of an expired reservation was already given back.
		held := res.EstMicros
		if status == domain.ReservationExpired {
			held = 0
		}
		if err := settlePeriods(ctx, tx, repo, res.BudgetPeriodIds, held, cost); err != nil {
			return err
		}
		return repo.SetReservationStatus(ctx, id, string(domain.ReservationCommitted))
	})
	if err != nil {
		return db.Ledger{}, err
	}
	return entry, nil
}

// Release gives a reservation's hold back after a failed call. Releasing a
// reservation that is no longer open does nothing.
func (s *Service) Release(ctx context.Context, reservationID string) error {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return err
	}
	id, err := parseReservationID(reservationID)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		res, err := repo.LockReservation(ctx, owner, id)
		if err != nil {
			return err
		}
		return giveBack(ctx, repo, res, domain.ReservationReleased)
	})
}

// ExpireReservations gives back the hold of every open reservation past its
// expiry, so a crashed caller does not hold budget forever. It returns how many
// it expired.
func (s *Service) ExpireReservations(ctx context.Context) (int, error) {
	expired := 0
	for {
		ids, err := s.expiredIDs(ctx)
		if err != nil {
			return expired, err
		}
		for _, id := range ids {
			ok, err := s.expireOne(ctx, id)
			if err != nil {
				return expired, err
			}
			if ok {
				expired++
			}
		}
		if len(ids) < expireBatchSize {
			return expired, nil
		}
	}
}

func (s *Service) expiredIDs(ctx context.Context) ([]uuid.UUID, error) {
	ids, err := store.New(s.pool).ExpiredReservationIDs(ctx, s.now(), expireBatchSize)
	if err != nil {
		return nil, fmt.Errorf("list expired reservations: %w", err)
	}
	return ids, nil
}

// expireOne expires one reservation in its own transaction, so that periods
// are never locked for more than one reservation at a time. It reports false
// when the reservation was settled in the meantime.
func (s *Service) expireOne(ctx context.Context, id uuid.UUID) (bool, error) {
	var expired bool
	err := s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		res, err := repo.LockReservationByID(ctx, id)
		if err != nil {
			return err
		}
		if !domain.ReservationStatus(res.Status).CanSettle() {
			return nil
		}
		expired = true
		return giveBack(ctx, repo, res, domain.ReservationExpired)
	})
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return expired, err
}

// giveBack returns an open reservation's hold and moves it to status. A
// reservation that is not open is left alone.
func giveBack(ctx context.Context, repo *store.Repo, res db.Reservation, status domain.ReservationStatus) error {
	if !domain.ReservationStatus(res.Status).CanSettle() {
		return nil
	}
	periods, err := repo.LockPeriods(ctx, res.BudgetPeriodIds)
	if err != nil {
		return fmt.Errorf("lock periods: %w", err)
	}
	for _, p := range periods {
		if err := repo.AddReserved(ctx, p.ID, -res.EstMicros); err != nil {
			return fmt.Errorf("give back reserved: %w", err)
		}
	}
	return repo.SetReservationStatus(ctx, res.ID, string(status))
}

// settlePeriods moves held from reserved to spent-by-cost on each period and
// notifies the thresholds the new spend crossed.
func settlePeriods(ctx context.Context, tx pgx.Tx, repo *store.Repo, periodIDs []uuid.UUID, held, cost int64) error {
	periods, err := repo.LockPeriods(ctx, periodIDs)
	if err != nil {
		return fmt.Errorf("lock periods: %w", err)
	}
	budgetIDs := make([]uuid.UUID, 0, len(periods))
	for _, p := range periods {
		budgetIDs = append(budgetIDs, p.BudgetID)
	}
	budgets, err := repo.BudgetsByID(ctx, budgetIDs)
	if err != nil {
		return err
	}
	for _, p := range periods {
		if err := repo.SettlePeriod(ctx, p.ID, held, cost); err != nil {
			return fmt.Errorf("settle period: %w", err)
		}
		if err := notifyThresholds(ctx, tx, repo, budgets[p.BudgetID], p, p.SpentMicros+cost); err != nil {
			return err
		}
	}
	return nil
}

// notifyThresholds writes cost.threshold_reached for each threshold spent has
// reached that the period has not announced yet.
func notifyThresholds(ctx context.Context, tx pgx.Tx, repo *store.Repo, b db.Budget, p db.BudgetPeriod, spent int64) error {
	if !b.Enabled {
		return nil
	}
	for _, threshold := range domain.ThresholdsReached(b.Thresholds, spent, b.LimitMicros) {
		isNew, err := repo.MarkThresholdNotified(ctx, p, threshold)
		if err != nil {
			return fmt.Errorf("mark threshold %d: %w", threshold, err)
		}
		if !isNew {
			continue
		}
		_, err = outbox.Write(ctx, tx, eventSource, eventThresholdHit, b.ID.String(), &sorobanv1.CostThresholdReached{
			BudgetId:    b.ID.String(),
			ScopeType:   wire.ScopeTypeToProto(domain.ScopeType(b.ScopeType)),
			ScopeValue:  b.ScopeValue,
			Period:      wire.PeriodToProto(domain.Period(b.Period)),
			Percent:     threshold,
			SpentMicros: spent,
			LimitMicros: b.LimitMicros,
			OwnerId:     b.OwnerID.String(),
		})
		if err != nil {
			return fmt.Errorf("write %s event: %w", eventThresholdHit, err)
		}
	}
	return nil
}

func parseReservationID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, invalid(ReasonInvalidRequest, "reservation_id %q is not a UUID", value)
	}
	return id, nil
}

// optional returns nil for an empty string, so that it is stored as NULL.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
