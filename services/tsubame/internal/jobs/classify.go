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
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
)

const (
	kindClassifyMessage = "classify_message"

	// classifyAttempts is how many tries a message gets.
	classifyAttempts = 3
	// classifyTimeout bounds one try, which may include a model call.
	classifyTimeout = 2 * time.Minute
	// classifyWorkers is how many messages are classified at once.
	classifyWorkers = 2
	// minSnooze is how long a budget-blocked job waits when soroban gave no
	// reset time.
	minSnooze = time.Hour
)

// ClassifyArgs are the args of classify_message. A message is queued once.
type ClassifyArgs struct {
	OwnerID   uuid.UUID `json:"owner_id"`
	MessageID uuid.UUID `json:"message_id" river:"unique"`
}

// Kind implements river.JobArgs.
func (ClassifyArgs) Kind() string { return kindClassifyMessage }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (ClassifyArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: classifyAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// Classifier is what the worker needs from the classification use case.
type Classifier interface {
	Classify(ctx context.Context, owner, messageID uuid.UUID) error
}

type classifyWorker struct {
	river.WorkerDefaults[ClassifyArgs]
	classifier Classifier
	log        *slog.Logger
	now        func() time.Time
}

func (w *classifyWorker) Timeout(*river.Job[ClassifyArgs]) time.Duration { return classifyTimeout }

// Work classifies one message. A budget refusal snoozes the job until the
// budget resets, which costs no try; a message that is gone ends it; any other
// error retries, and after the last try the message stays unclassified.
func (w *classifyWorker) Work(ctx context.Context, job *river.Job[ClassifyArgs]) error {
	// Calls to kagami and soroban are signed with the owner's identity.
	ctx = authz.WithIdentity(ctx, authz.Identity{
		OwnerID: job.Args.OwnerID.String(), RequestID: fmt.Sprintf("classify-message-%d", job.ID),
	})
	err := w.classifier.Classify(ctx, job.Args.OwnerID, job.Args.MessageID)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return river.JobCancel(fmt.Errorf("message is gone: %w", err))
	}
	var budget *llm.BudgetError
	if errors.As(err, &budget) {
		wait := minSnooze
		if !budget.ResetsAt.IsZero() {
			wait = max(budget.ResetsAt.Sub(w.now()), time.Minute)
		}
		w.log.Info("classification waits for budget", slog.String("message_id", job.Args.MessageID.String()), slog.Duration("wait", wait))
		return river.JobSnooze(wait)
	}
	w.log.Warn("classification failed", slog.String("message_id", job.Args.MessageID.String()),
		slog.Int("attempt", job.Attempt), slog.Any("error", err))
	return err
}

// RiverQueue inserts classify_message jobs inside the caller's transaction, so
// a stored message and its job succeed or fail together. It implements
// app.ClassifyQueue.
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

// EnqueueClassify implements app.ClassifyQueue.
func (q *RiverQueue) EnqueueClassify(ctx context.Context, tx pgx.Tx, owner, messageID uuid.UUID) error {
	if _, err := q.client.InsertTx(ctx, tx, ClassifyArgs{OwnerID: owner, MessageID: messageID}, nil); err != nil {
		return fmt.Errorf("enqueue classify_message: %w", err)
	}
	return nil
}
