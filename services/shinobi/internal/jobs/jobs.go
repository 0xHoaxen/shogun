// Package jobs holds shinobi's River workers: the minutely check for sources
// that are due, the run of one source and the scoring of one posting.
//
// The workers share the outbox relay's River client (see relay.Config), so they
// are handed over as a Setup rather than started here.
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

	"github.com/0xHoaxen/shogun/pkg/postgres"
	_ "github.com/0xHoaxen/shogun/pkg/schedule" // embeds the time zone database Location needs
	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
)

const (
	// Queue is the River queue of shinobi's own jobs.
	Queue = "shinobi"

	// locationName is the zone the owner's source schedules are read in.
	locationName = "Asia/Kolkata"

	kindDueSources = "due_sources"
	kindRunSource  = "run_source"

	dueEvery     = time.Minute
	minuteLayout = "2006-01-02T15:04"
	// queueWorkers lets a few sources be read at once; each host is still spaced.
	queueWorkers = 2
)

// Scheduler finds the sources that are due.
type Scheduler interface {
	DueSources(ctx context.Context, now time.Time, loc *time.Location) ([]app.DueRun, error)
}

// Runner reads one source for its owner.
type Runner interface {
	RunSourceFor(ctx context.Context, owner, id uuid.UUID) (app.RunResult, error)
}

// Enqueuer starts the run of a due source. A slot is started once, however often
// it is offered.
type Enqueuer interface {
	EnqueueRun(ctx context.Context, run app.DueRun) error
}

// DueSourcesArgs are the args of due_sources. Minute is the minute it checks, so
// two checks of one minute are one job.
type DueSourcesArgs struct {
	Minute string `json:"minute"`
}

// Kind implements river.JobArgs.
func (DueSourcesArgs) Kind() string { return kindDueSources }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (DueSourcesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// RunSourceArgs are the args of run_source. It is unique per source and slot, so
// a slot is read once however often it is offered, and never retried: a failure
// is kept with the source and the next slot tries again.
type RunSourceArgs struct {
	OwnerID  uuid.UUID `json:"owner_id"`
	SourceID uuid.UUID `json:"source_id"`
	Slot     string    `json:"slot"`
}

// Kind implements river.JobArgs.
func (RunSourceArgs) Kind() string { return kindRunSource }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (RunSourceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type dueWorker struct {
	river.WorkerDefaults[DueSourcesArgs]
	scheduler Scheduler
	enqueuer  Enqueuer
	loc       *time.Location
	now       func() time.Time
	log       *slog.Logger
}

// Work implements river.Worker. A source that cannot be queued does not stop
// the others; the failures are returned together.
func (w *dueWorker) Work(ctx context.Context, _ *river.Job[DueSourcesArgs]) error {
	due, err := w.scheduler.DueSources(ctx, w.now(), w.loc)
	if err != nil {
		return fmt.Errorf("find due sources: %w", err)
	}
	var errs []error
	for _, run := range due {
		if err := w.enqueuer.EnqueueRun(ctx, run); err != nil {
			errs = append(errs, fmt.Errorf("queue source %s: %w", run.SourceID, err))
		}
	}
	if len(due) > 0 {
		w.log.Info("sources are due", slog.Int("count", len(due)))
	}
	return errors.Join(errs...)
}

type runWorker struct {
	river.WorkerDefaults[RunSourceArgs]
	runner Runner
	log    *slog.Logger
}

// Work implements river.Worker. A source that could not be read is recorded with
// the source and is not an error of the job.
func (w *runWorker) Work(ctx context.Context, job *river.Job[RunSourceArgs]) error {
	res, err := w.runner.RunSourceFor(ctx, job.Args.OwnerID, job.Args.SourceID)
	var runErr *app.RunError
	switch {
	case err == nil:
		w.log.Info("source read", slog.String("source_id", job.Args.SourceID.String()),
			slog.Int("fetched", res.Fetched), slog.Int("added", res.Added), slog.Int("skipped", res.Skipped))
		return nil
	case errors.Is(err, app.ErrSourceNotFound):
		return river.JobCancel(err)
	case errors.As(err, &runErr):
		w.log.Warn("source could not be read", slog.String("source_id", job.Args.SourceID.String()), slog.String("code", runErr.Code))
		return nil
	default:
		return fmt.Errorf("run source %s: %w", job.Args.SourceID, err)
	}
}

// Setup is what the outbox relay needs to run shinobi's jobs on its River
// client: pass the fields to relay.Config.
type Setup struct {
	Workers      func(*river.Workers)
	Queues       map[string]river.QueueConfig
	PeriodicJobs []*river.PeriodicJob
}

// Location loads the zone source schedules are read in.
func Location() (*time.Location, error) {
	loc, err := time.LoadLocation(locationName)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", locationName, err)
	}
	return loc, nil
}

func dueArgs(now func() time.Time) func() (river.JobArgs, *river.InsertOpts) {
	return func() (river.JobArgs, *river.InsertOpts) {
		return DueSourcesArgs{Minute: now().UTC().Format(minuteLayout)}, nil
	}
}

// NewSetup builds the jobs. now is the clock, injected for tests.
func NewSetup(scheduler Scheduler, runner Runner, scorer Scorer, enqueuer Enqueuer, loc *time.Location, now func() time.Time, log *slog.Logger) Setup {
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &dueWorker{scheduler: scheduler, enqueuer: enqueuer, loc: loc, now: now, log: log})
			river.AddWorker(ws, &runWorker{runner: runner, log: log})
			river.AddWorker(ws, &scoreWorker{scorer: scorer, now: now, log: log})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: queueWorkers}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(dueEvery), dueArgs(now), nil),
		},
	}
}

// RiverQueue inserts run_source jobs. It implements Enqueuer.
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

// EnqueueRun implements Enqueuer.
func (q *RiverQueue) EnqueueRun(ctx context.Context, run app.DueRun) error {
	_, err := q.client.Insert(ctx, RunSourceArgs{OwnerID: run.OwnerID, SourceID: run.SourceID, Slot: run.Slot}, nil)
	if err != nil {
		return fmt.Errorf("enqueue run_source: %w", err)
	}
	return nil
}
