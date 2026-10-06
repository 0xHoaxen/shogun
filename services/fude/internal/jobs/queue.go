package jobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
)

// RiverQueue inserts generate_draft jobs inside the caller's transaction, so a
// draft and its job succeed or fail together. It implements app.Queue.
type RiverQueue struct {
	client *river.Client[pgx.Tx]
}

// NewRiverQueue returns a queue on pool's schema. Its River client only
// inserts; the relay's client runs the jobs.
func NewRiverQueue(pool *pgxpool.Pool) (*RiverQueue, error) {
	schema, err := postgres.SchemaOf(pool)
	if err != nil {
		return nil, fmt.Errorf("river queue: %w", err)
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: schema})
	if err != nil {
		return nil, fmt.Errorf("river queue: %w", err)
	}
	return &RiverQueue{client: client}, nil
}

// EnqueueGenerate implements app.Queue.
func (q *RiverQueue) EnqueueGenerate(ctx context.Context, tx pgx.Tx, args app.GenerateArgs) error {
	_, err := q.client.InsertTx(ctx, tx, GenerateArgs{
		OwnerID: args.OwnerID, DraftID: args.DraftID, Version: args.Version, ExtraContext: args.ExtraContext,
	}, nil)
	if err != nil {
		return fmt.Errorf("enqueue generate_draft: %w", err)
	}
	return nil
}
