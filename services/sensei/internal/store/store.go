package store

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/sensei/internal/store/db"
)

// ErrNotFound means no row matched.
var ErrNotFound = errors.New("store: not found")

// DBTX is a pool or a transaction. Build a Repo on a pgx.Tx to write a fact in
// the same transaction as the inbox row of its event.
type DBTX = db.DBTX

// Repo reads and writes the sensei tables. It holds no state besides the
// connection it was built on.
type Repo struct {
	q *db.Queries
	d DBTX
}

// New returns a Repo that runs on d.
func New(d DBTX) *Repo { return &Repo{q: db.New(d), d: d} }

// NewID returns a new UUIDv7.
func NewID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

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

func marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("store: marshal: %w", err)
	}
	return raw, nil
}
