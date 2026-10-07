package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
)

const (
	kindScorePosting = "score_posting"
	scoreMaxAttempts = 3
	// minBudgetSnooze is how long scoring waits for a budget that gave no reset.
	minBudgetSnooze = time.Hour
)

// Scorer scores one posting.
type Scorer interface {
	ScorePosting(ctx context.Context, in app.ScoreInput) error
}

// ScorePostingArgs are the args of score_posting. A posting has one job per
// version of its owner's preferences, so a new score after they change is not
// mistaken for the first.
type ScorePostingArgs struct {
	OwnerID   uuid.UUID `json:"owner_id"`
	PostingID uuid.UUID `json:"posting_id"`
	Version   int64     `json:"version"`
}

// Kind implements river.JobArgs.
func (ScorePostingArgs) Kind() string { return kindScorePosting }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (ScorePostingArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: scoreMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type scoreWorker struct {
	river.WorkerDefaults[ScorePostingArgs]
	scorer Scorer
	now    func() time.Time
	log    *slog.Logger
}

// Work implements river.Worker. A refused budget snoozes the job until the
// budget resets; anything else is retried.
func (w *scoreWorker) Work(ctx context.Context, job *river.Job[ScorePostingArgs]) error {
	// Metering calls are signed with the owner's identity.
	ctx = authz.WithIdentity(ctx, authz.Identity{
		OwnerID: job.Args.OwnerID.String(), RequestID: fmt.Sprintf("score-posting-%d", job.ID),
	})
	err := w.scorer.ScorePosting(ctx, app.ScoreInput{OwnerID: job.Args.OwnerID, PostingID: job.Args.PostingID, Version: job.Args.Version})
	var budget *llm.BudgetError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &budget):
		wait := minBudgetSnooze
		if !budget.ResetsAt.IsZero() {
			wait = max(budget.ResetsAt.Sub(w.now()), time.Minute)
		}
		w.log.Info("scoring waits for budget", slog.String("posting_id", job.Args.PostingID.String()), slog.Duration("wait", wait))
		return river.JobSnooze(wait)
	default:
		return fmt.Errorf("score posting %s: %w", job.Args.PostingID, err)
	}
}

// EnqueueScore inserts a score_posting job in tx. It implements app.Queue.
func (q *RiverQueue) EnqueueScore(ctx context.Context, tx pgx.Tx, in app.ScoreInput) error {
	_, err := q.client.InsertTx(ctx, tx, ScorePostingArgs{OwnerID: in.OwnerID, PostingID: in.PostingID, Version: in.Version}, nil)
	if err != nil {
		return fmt.Errorf("enqueue score_posting: %w", err)
	}
	return nil
}
