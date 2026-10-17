// Package app holds the sensei use cases.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/owners"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store"
)

const (
	// RollupWindowDays is how many days back each rollup rebuilds, so a late
	// event still lands while older days are left alone.
	RollupWindowDays = 45
)

// ErrNoOwner means the call carries no identity, or one whose owner is not a
// UUID. Transport maps it to PermissionDenied.
var ErrNoOwner = errors.New("app: call has no valid owner")

// Service runs the analytics use cases.
type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewService returns a Service on pool. now is the clock; nil means time.Now.
func NewService(pool *pgxpool.Pool, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, now: now}
}

// RecordFact stores the fact an event caused, in tx, the transaction that also
// marks the event as seen. A fact that can never be stored is
// domain.ErrInvalidFact. A repeated event stores nothing.
func (s *Service) RecordFact(ctx context.Context, tx pgx.Tx, f domain.Fact) error {
	clean, err := f.Validate()
	if err != nil {
		return fmt.Errorf("record fact: %w", err)
	}
	if _, err := store.New(tx).InsertFact(ctx, clean); err != nil {
		return fmt.Errorf("record fact: %w", err)
	}
	return nil
}

// today is the current date in loc, as midnight UTC, which is how a date column
// reads back.
func (s *Service) today(loc *time.Location) time.Time {
	local := s.now().In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// ownerLocation is the zone the owner's days are counted in: their own once
// torii has announced them, the default until then.
func (s *Service) ownerLocation(ctx context.Context, owner uuid.UUID) (*time.Location, error) {
	o, err := owners.Get(ctx, s.pool, owner)
	if errors.Is(err, owners.ErrNotFound) {
		return owners.Owner{Timezone: owners.DefaultTimezone}.Location(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("owner zone: %w", err)
	}
	return o.Location(), nil
}

// Rollup rebuilds one owner's daily rollups for the last RollupWindowDays days
// from their facts, in one transaction, counting days in the owner's own time
// zone. It is idempotent: the same facts always give the same rollups. An owner
// torii has not announced yet is counted in the default zone, so no analytics
// are lost; a deleted owner is skipped.
func (s *Service) Rollup(ctx context.Context, owner uuid.UUID) error {
	o, err := owners.Get(ctx, s.pool, owner)
	switch {
	case errors.Is(err, owners.ErrNotFound):
		o = owners.Owner{ID: owner, Timezone: owners.DefaultTimezone}
	case err != nil:
		return fmt.Errorf("rollup: %w", err)
	case o.Status == owners.StatusDeleted:
		return nil
	}
	from := s.today(o.Location()).AddDate(0, 0, -RollupWindowDays)
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return store.New(tx).RebuildRollups(ctx, owner, o.Timezone, from)
	})
	if err != nil {
		return fmt.Errorf("rollup: %w", err)
	}
	return nil
}

// ActiveOwners lists the owners torii has told sensei about whose rollups may be
// due.
func (s *Service) ActiveOwners(ctx context.Context) ([]owners.Owner, error) {
	return owners.ListActive(ctx, s.pool)
}

func ownerFrom(ctx context.Context) (uuid.UUID, error) {
	id, ok := authz.FromContext(ctx)
	if !ok {
		return uuid.Nil, ErrNoOwner
	}
	owner, err := uuid.Parse(id.OwnerID)
	if err != nil {
		return uuid.Nil, ErrNoOwner
	}
	return owner, nil
}
