// Package store holds the soroban Postgres access through sqlc.
package store

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
)

// ErrNotFound means the row asked for does not exist.
var ErrNotFound = errors.New("store: not found")

// DBTX is a pool or a transaction. Build a Repo on a pgx.Tx to run several
// writes, and an outbox event, atomically.
type DBTX = db.DBTX

// Repo reads and writes the soroban tables. It holds no state besides the
// connection it was built on.
type Repo struct {
	q *db.Queries
}

// New returns a Repo that runs on d.
func New(d DBTX) *Repo {
	return &Repo{q: db.New(d)}
}

// NewID returns a new UUIDv7.
func NewID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// mapErr turns driver errors into the package's sentinel errors.
func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
