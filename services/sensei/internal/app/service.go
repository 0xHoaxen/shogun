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
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store"
)

const (
	// istOffset is the offset of Asia/Kolkata, the zone days are counted in.
	// India has no daylight saving time, so a fixed offset is exact.
	istOffset = 5*time.Hour + 30*time.Minute
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

// today is the current date in the owner's zone, as midnight UTC, which is how
// a date column reads back.
func (s *Service) today() time.Time {
	local := s.now().UTC().Add(istOffset)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// Rollup rebuilds the daily rollups for the last RollupWindowDays days from the
// facts, in one transaction. It is idempotent: the same facts always give the
// same rollups.
func (s *Service) Rollup(ctx context.Context) error {
	from := s.today().AddDate(0, 0, -RollupWindowDays)
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return store.New(tx).RebuildRollups(ctx, from)
	})
	if err != nil {
		return fmt.Errorf("rollup: %w", err)
	}
	return nil
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
