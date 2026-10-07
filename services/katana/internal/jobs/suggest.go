package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

const (
	kindSuggest        = "suggest"
	suggestMaxAttempts = 3
	// minBudgetSnooze is how long a run waits for a budget that gave no reset.
	minBudgetSnooze = time.Hour
)

// Suggester writes suggestions for one run.
type Suggester interface {
	Suggest(ctx context.Context, in app.SuggestInput) (int, error)
}

// SuggestArgs are the args of suggest. A run is unique per snapshot, or per
// finished item, so a retry or a redelivered event never doubles it.
type SuggestArgs struct {
	OwnerID    uuid.UUID           `json:"owner_id"`
	SnapshotID *uuid.UUID          `json:"snapshot_id,omitempty"`
	Learned    *domain.LearnedItem `json:"learned,omitempty"`
}

// Kind implements river.JobArgs.
func (SuggestArgs) Kind() string { return kindSuggest }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (SuggestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: suggestMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

func (a SuggestArgs) input() app.SuggestInput {
	return app.SuggestInput{OwnerID: a.OwnerID, SnapshotID: a.SnapshotID, Learned: a.Learned}
}

type suggestWorker struct {
	river.WorkerDefaults[SuggestArgs]
	suggester Suggester
	now       func() time.Time
	log       *slog.Logger
}

// Work implements river.Worker. A refused budget snoozes the run until the
// budget resets; a model that is not set up cancels it, since a retry cannot
// help; anything else is retried.
func (w *suggestWorker) Work(ctx context.Context, job *river.Job[SuggestArgs]) error {
	// Metering calls are signed with the owner's identity.
	ctx = authz.WithIdentity(ctx, authz.Identity{
		OwnerID: job.Args.OwnerID.String(), RequestID: fmt.Sprintf("suggest-%d", job.ID),
	})
	n, err := w.suggester.Suggest(ctx, job.Args.input())
	var budget *llm.BudgetError
	switch {
	case err == nil:
		w.log.Info("suggestions written", slog.String("owner_id", job.Args.OwnerID.String()), slog.Int("count", n))
		return nil
	case errors.Is(err, app.ErrSuggestOff):
		return river.JobCancel(err)
	case errors.As(err, &budget):
		wait := minBudgetSnooze
		if !budget.ResetsAt.IsZero() {
			wait = max(budget.ResetsAt.Sub(w.now()), time.Minute)
		}
		w.log.Info("suggestions wait for budget", slog.String("owner_id", job.Args.OwnerID.String()), slog.Duration("wait", wait))
		return river.JobSnooze(wait)
	default:
		return fmt.Errorf("suggest for owner %s: %w", job.Args.OwnerID, err)
	}
}

// RiverQueue inserts suggest jobs inside the caller's transaction. It
// implements app.Queue.
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

// EnqueueSuggest implements app.Queue.
func (q *RiverQueue) EnqueueSuggest(ctx context.Context, tx pgx.Tx, in app.SuggestInput) error {
	_, err := q.client.InsertTx(ctx, tx, SuggestArgs{OwnerID: in.OwnerID, SnapshotID: in.SnapshotID, Learned: in.Learned}, nil)
	if err != nil {
		return fmt.Errorf("enqueue suggest: %w", err)
	}
	return nil
}
