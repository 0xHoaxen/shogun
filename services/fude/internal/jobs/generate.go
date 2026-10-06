package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
)

const (
	// Queue is the River queue of fude's own jobs.
	Queue = "fude"

	kindGenerateDraft = "generate_draft"

	// workers is how many drafts are written at once.
	workers = 2
	// maxAttempts is how many tries a draft gets before it is marked failed.
	maxAttempts = 5
	// generateTimeout bounds one try; a long model call must not be cut at
	// River's one-minute default.
	generateTimeout = 3 * time.Minute
	// minSnooze is how long a budget-blocked job waits when soroban gave no
	// reset time.
	minSnooze = time.Hour
)

// GenerateArgs are the args of generate_draft. A draft and version are queued
// once, however many times they are asked for.
type GenerateArgs struct {
	OwnerID      uuid.UUID `json:"owner_id"`
	DraftID      uuid.UUID `json:"draft_id" river:"unique"`
	Version      int32     `json:"version" river:"unique"`
	ExtraContext string    `json:"extra_context,omitempty"`
}

// Kind implements river.JobArgs.
func (GenerateArgs) Kind() string { return kindGenerateDraft }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (GenerateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       Queue,
		MaxAttempts: maxAttempts,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

func (a GenerateArgs) appArgs() app.GenerateArgs {
	return app.GenerateArgs{OwnerID: a.OwnerID, DraftID: a.DraftID, Version: a.Version, ExtraContext: a.ExtraContext}
}

// Drafter is what the worker needs from the generation use case.
type Drafter interface {
	Generate(ctx context.Context, args app.GenerateArgs) error
	Fail(ctx context.Context, args app.GenerateArgs, reason string) error
}

type generateWorker struct {
	river.WorkerDefaults[GenerateArgs]
	drafter Drafter
	log     *slog.Logger
	now     func() time.Time
}

func (w *generateWorker) Timeout(*river.Job[GenerateArgs]) time.Duration { return generateTimeout }

// Work writes one version. A budget refusal snoozes the job until the budget
// resets, which costs no attempt. Other errors retry; on the last attempt the
// draft is marked failed and the job ends.
func (w *generateWorker) Work(ctx context.Context, job *river.Job[GenerateArgs]) error {
	args := job.Args.appArgs()
	// Metering and kagami calls are signed with the owner's identity.
	ctx = authz.WithIdentity(ctx, authz.Identity{
		OwnerID: args.OwnerID.String(), RequestID: fmt.Sprintf("generate-draft-%d", job.ID),
	})

	err := w.drafter.Generate(ctx, args)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return river.JobCancel(fmt.Errorf("draft is gone: %w", err))
	}

	var budget *llm.BudgetError
	if errors.As(err, &budget) {
		wait := minSnooze
		if !budget.ResetsAt.IsZero() {
			wait = max(budget.ResetsAt.Sub(w.now()), time.Minute)
		}
		w.log.Info("generation waits for budget", slog.String("draft_id", args.DraftID.String()), slog.Duration("wait", wait))
		return river.JobSnooze(wait)
	}

	reason := app.FailureReason(err)
	w.log.Warn("generation failed", slog.String("draft_id", args.DraftID.String()),
		slog.Int("attempt", job.Attempt), slog.String("reason", reason), slog.Any("error", err))
	if job.Attempt < job.MaxAttempts {
		return err
	}
	if failErr := w.drafter.Fail(ctx, args, reason); failErr != nil {
		return fmt.Errorf("record failure: %w", failErr)
	}
	return nil
}

// Setup is what the outbox relay needs to run fude's jobs on its River client:
// pass the fields to relay.Config.
type Setup struct {
	Workers func(*river.Workers)
	Queues  map[string]river.QueueConfig
}

// NewSetup builds the workers: generation always, and sample embedding when
// there is an embedder to run it.
func NewSetup(drafter Drafter, embedder SampleEmbedder, log *slog.Logger) Setup {
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &generateWorker{drafter: drafter, log: log, now: time.Now})
			if embedder != nil {
				river.AddWorker(ws, &embedWorker{embedder: embedder, log: log})
			}
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: workers}},
	}
}
