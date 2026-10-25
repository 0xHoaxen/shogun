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
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
)

const (
	kindJobSkills        = "job_skills"
	jobSkillsMaxAttempts = 3
)

// JobSkillReader reads one saved job for the skills it asks for.
type JobSkillReader interface {
	ExtractJobSkills(ctx context.Context, owner, job uuid.UUID) error
}

// JobSkillsArgs are the args of job_skills. A job is read once, so the job is
// unique by its args and a redelivered job.added adds none.
type JobSkillsArgs struct {
	OwnerID uuid.UUID `json:"owner_id"`
	JobID   uuid.UUID `json:"job_id"`
}

// Kind implements river.JobArgs.
func (JobSkillsArgs) Kind() string { return kindJobSkills }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (JobSkillsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: jobSkillsMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type jobSkillsWorker struct {
	river.WorkerDefaults[JobSkillsArgs]
	reader JobSkillReader
	now    func() time.Time
	log    *slog.Logger
}

// Work implements river.Worker. A user with no AI provider, or a katana with no
// model, cancels the job, since a retry cannot help; a refused budget snoozes it
// until the budget resets; anything else is retried.
func (w *jobSkillsWorker) Work(ctx context.Context, job *river.Job[JobSkillsArgs]) error {
	ctx = authz.WithIdentity(ctx, authz.Identity{
		OwnerID: job.Args.OwnerID.String(), RequestID: fmt.Sprintf("job-skills-%d", job.ID),
	})
	err := w.reader.ExtractJobSkills(ctx, job.Args.OwnerID, job.Args.JobID)
	var budget *llm.BudgetError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, app.ErrJobSkillsOff), errors.Is(err, llm.ErrProviderNotConfigured):
		return river.JobCancel(err)
	case errors.As(err, &budget):
		wait := minBudgetSnooze
		if !budget.ResetsAt.IsZero() {
			wait = max(budget.ResetsAt.Sub(w.now()), time.Minute)
		}
		return river.JobSnooze(wait)
	default:
		return fmt.Errorf("job skills for job %s: %w", job.Args.JobID, err)
	}
}

// EnqueueJobSkills implements app.Queue.
func (q *RiverQueue) EnqueueJobSkills(ctx context.Context, tx pgx.Tx, owner, job uuid.UUID) error {
	if _, err := q.client.InsertTx(ctx, tx, JobSkillsArgs{OwnerID: owner, JobID: job}, nil); err != nil {
		return fmt.Errorf("enqueue job_skills: %w", err)
	}
	return nil
}

// WithJobSkills returns the setup with the job_skills worker added.
func (s Setup) WithJobSkills(reader JobSkillReader, now func() time.Time, log *slog.Logger) Setup {
	workers := s.Workers
	s.Workers = func(ws *river.Workers) {
		workers(ws)
		river.AddWorker(ws, &jobSkillsWorker{reader: reader, now: now, log: log})
	}
	return s
}
