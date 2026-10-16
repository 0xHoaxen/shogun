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

// GetProvider returns the owner's provider, or ErrNotFound.
func (r *Repo) GetProvider(ctx context.Context, owner uuid.UUID) (db.Provider, error) {
	row, err := r.q.GetProvider(ctx, owner)
	if err != nil {
		return db.Provider{}, mapErr(err)
	}
	return row, nil
}

// GetProviderForUpdate returns the owner's provider and locks its row until the
// transaction ends, so one owner's reservations are checked one at a time.
func (r *Repo) GetProviderForUpdate(ctx context.Context, owner uuid.UUID) (db.Provider, error) {
	row, err := r.q.GetProviderForUpdate(ctx, owner)
	if err != nil {
		return db.Provider{}, mapErr(err)
	}
	return row, nil
}

// TokensUsed returns every token the owner used in [from, to).
func (r *Repo) TokensUsed(ctx context.Context, owner uuid.UUID, from, to time.Time) (int64, error) {
	n, err := r.q.SumLedgerTokens(ctx, db.SumLedgerTokensParams{OwnerID: owner, FromTime: from, ToTime: to})
	if err != nil {
		return 0, fmt.Errorf("sum tokens used: %w", err)
	}
	return n, nil
}

// TokensInFlight returns the tokens held by the owner's calls that have not
// finished at now.
func (r *Repo) TokensInFlight(ctx context.Context, owner uuid.UUID, now time.Time) (int64, error) {
	n, err := r.q.SumOpenReservedTokens(ctx, db.SumOpenReservedTokensParams{OwnerID: owner, Now: now})
	if err != nil {
		return 0, fmt.Errorf("sum tokens in flight: %w", err)
	}
	return n, nil
}

// InsertProvider stores the owner's first provider. A second one is ErrDuplicate.
func (r *Repo) InsertProvider(ctx context.Context, arg db.InsertProviderParams) (db.Provider, error) {
	row, err := r.q.InsertProvider(ctx, arg)
	if err != nil {
		return db.Provider{}, fmt.Errorf("insert provider: %w", mapErr(err))
	}
	return row, nil
}

// UpdateProvider changes the owner's provider if version is the stored one. A
// stale version, or no provider, is ErrVersionConflict.
func (r *Repo) UpdateProvider(ctx context.Context, arg db.UpdateProviderParams) (db.Provider, error) {
	row, err := r.q.UpdateProvider(ctx, arg)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Provider{}, ErrVersionConflict
	}
	if err != nil {
		return db.Provider{}, fmt.Errorf("update provider: %w", err)
	}
	return row, nil
}

// DeleteProvider removes the owner's provider. It reports whether there was one.
func (r *Repo) DeleteProvider(ctx context.Context, owner uuid.UUID) (bool, error) {
	n, err := r.q.DeleteProvider(ctx, owner)
	if err != nil {
		return false, fmt.Errorf("delete provider: %w", err)
	}
	return n > 0, nil
}
