package jobs

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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
	embed  bool
}

// NewRiverQueue returns a queue on pool's schema. Its River client only
// inserts; the relay's client runs the jobs. With embeddings off, voice
// samples are stored without an embedding job.
func NewRiverQueue(pool *pgxpool.Pool, embeddings bool) (*RiverQueue, error) {
	schema, err := postgres.SchemaOf(pool)
	if err != nil {
		return nil, fmt.Errorf("river queue: %w", err)
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: schema})
	if err != nil {
		return nil, fmt.Errorf("river queue: %w", err)
	}
	return &RiverQueue{client: client, embed: embeddings}, nil
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

// EnqueueEmbed implements app.Queue.
func (q *RiverQueue) EnqueueEmbed(ctx context.Context, tx pgx.Tx, sampleID uuid.UUID) error {
	if !q.embed {
		return nil
	}
	if _, err := q.client.InsertTx(ctx, tx, EmbedArgs{SampleID: sampleID}, nil); err != nil {
		return fmt.Errorf("enqueue embed_voice_sample: %w", err)
	}
	return nil
}
