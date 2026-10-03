// Package store holds the kagami Postgres access through sqlc.
package store

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

// Errors the app layer maps to gRPC status codes.
var (
	ErrNotFound         = errors.New("store: not found")
	ErrVersionConflict  = errors.New("store: version conflict")
	ErrDuplicate        = errors.New("store: duplicate")
	ErrInvalidPageToken = errors.New("store: invalid page token")
)

// uniqueViolation is the Postgres SQLSTATE for a unique index violation.
const uniqueViolation = "23505"

// DBTX is a pool or a transaction. Build a Repo on a pgx.Tx to run several
// writes, and an outbox event, atomically.
type DBTX = db.DBTX

// Repo reads and writes the kagami tables. It holds no state besides the
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
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return fmt.Errorf("%w: %s", ErrDuplicate, pgErr.ConstraintName)
	}
	return err
}

// staleOrMissing explains why a versioned update touched no row: the row is
// there with another version, or it is not there at all.
func staleOrMissing(err error, exists func() error) error {
	if !errors.Is(err, pgx.ErrNoRows) {
		return mapErr(err)
	}
	if lookupErr := exists(); lookupErr != nil {
		return mapErr(lookupErr)
	}
	return ErrVersionConflict
}
