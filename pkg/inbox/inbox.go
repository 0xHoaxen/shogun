// Package inbox makes event consumption idempotent: each event id is recorded
// in the service's inbox table in the same transaction as the handler's work.
package inbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/postgres"
)

const insertSQL = `INSERT INTO inbox (event_id) VALUES ($1) ON CONFLICT (event_id) DO NOTHING`

// Handle runs fn at most once per successfully handled event id. In one
// transaction it records env.Id in the inbox; if the id was already recorded
// it returns nil without calling fn. If fn returns an error the transaction
// rolls back, including the inbox row, so a retry runs fn again.
func Handle(ctx context.Context, pool *pgxpool.Pool, env *eventsv1.Envelope, fn func(context.Context, pgx.Tx) error) error {
	if env == nil || fn == nil {
		return errors.New("inbox: nil envelope or handler")
	}
	id, err := uuid.Parse(env.GetId())
	if err != nil {
		return fmt.Errorf("inbox: invalid event id: %w", err)
	}
	return postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, insertSQL, id)
		if err != nil {
			return fmt.Errorf("inbox: record event: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		return fn(ctx, tx)
	})
}
