package store

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
)

// Errors the app layer maps to gRPC status codes.
var (
	ErrNotFound         = errors.New("store: not found")
	ErrInvalidPageToken = errors.New("store: invalid page token")
	ErrVersionConflict  = errors.New("store: version is stale")
)

// DBTX is a pool or a transaction. Build a Repo on a pgx.Tx to write a row in
// the same transaction as the outbox row of its event.
type DBTX = db.DBTX

// Repo reads and writes the katana tables. It holds no state besides the
// connection it was built on.
type Repo struct {
	q *db.Queries
	d DBTX
}

// New returns a Repo that runs on d.
func New(d DBTX) *Repo {
	return &Repo{q: db.New(d), d: d}
}

// NewID returns a new UUIDv7.
func NewID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("store: %s: %w", op, mapErr(err))
}
