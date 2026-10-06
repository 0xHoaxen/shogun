package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

// ErrNotFound means no row matches, or it belongs to another owner.
var ErrNotFound = errors.New("store: not found")

// DBTX is a pool or a transaction.
type DBTX = db.DBTX

// Repo reads and writes the tsubame tables. It holds no state besides the
// connection it was built on.
type Repo struct {
	q *db.Queries
}

// New returns a Repo that runs on d.
func New(d DBTX) *Repo { return &Repo{q: db.New(d)} }

// NewID returns a new UUIDv7.
func NewID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// UpsertAccount stores a connected account. An address connected before keeps
// its id and sync cursor and gets the new token.
func (r *Repo) UpsertAccount(ctx context.Context, arg db.UpsertAccountParams) (db.Account, error) {
	a, err := r.q.UpsertAccount(ctx, arg)
	if err != nil {
		return db.Account{}, fmt.Errorf("upsert account: %w", mapErr(err))
	}
	return a, nil
}

// GetAccount returns one account of the owner.
func (r *Repo) GetAccount(ctx context.Context, owner, id uuid.UUID) (db.Account, error) {
	a, err := r.q.GetAccount(ctx, db.GetAccountParams{ID: id, OwnerID: owner})
	return a, mapErr(err)
}

// ListAccounts returns the owner's accounts, oldest first.
func (r *Repo) ListAccounts(ctx context.Context, owner uuid.UUID) ([]db.Account, error) {
	as, err := r.q.ListAccounts(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", mapErr(err))
	}
	return as, nil
}

// SetAccountStatus changes an account's status. It is ErrNotFound when the
// account is not the owner's.
func (r *Repo) SetAccountStatus(ctx context.Context, owner, id uuid.UUID, status string) error {
	n, err := r.q.SetAccountStatus(ctx, db.SetAccountStatusParams{ID: id, OwnerID: owner, Status: status})
	if err != nil {
		return fmt.Errorf("set account status: %w", mapErr(err))
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
