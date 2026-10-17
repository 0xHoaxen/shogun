// Package jobs holds sensei's River worker: the nightly rollup.
//
// The rollup shares the outbox relay's River client (see relay.Config), so it is
// handed over as a Setup rather than started here.
//
// Each owner is rolled up at rollupHour:rollupMinute on their own clock. A tick
// every tickEvery asks who has reached that time and queues one rollup job per
// owner and local date, unique by args, so an owner is rolled up once a day
// whatever their time zone.
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

	"github.com/0xHoaxen/shogun/pkg/owners"
)

const (
	// Queue is the River queue of sensei's own jobs.
	Queue = "sensei"

	kindRollup     = "rollup"
	kindRollupTick = "rollup_tick"

	rollupHour, rollupMinute = 1, 0
	rollupMaxAttempts        = 3

	// tickEvery is how often sensei looks for owners whose rollup time has come.
	tickEvery = 15 * time.Minute

	minuteLayout = "2006-01-02T15:04"
)

// Roller rebuilds one owner's daily rollups.
type Roller interface {
	Rollup(ctx context.Context, owner uuid.UUID) error
}

// OwnerLister lists the owners whose rollup may be due.
type OwnerLister interface {
	ActiveOwners(ctx context.Context) ([]owners.Owner, error)
}

// Enqueuer queues the rollup of one owner for one local date.
type Enqueuer interface {
	EnqueueRollup(ctx context.Context, owner uuid.UUID, date string) error
}

// RollupArgs are the args of rollup. Date is the owner's local calendar date,
// YYYY-MM-DD; unique-by-args makes the job run once per owner and date.
type RollupArgs struct {
	OwnerID string `json:"owner_id"`
	Date    string `json:"date"`
}

// Kind implements river.JobArgs.
func (RollupArgs) Kind() string { return kindRollup }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (RollupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: rollupMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// RollupTickArgs are the args of rollup_tick; Minute is the UTC minute it was
// queued for, so a tick is queued once however many replicas run.
type RollupTickArgs struct {
	Minute string `json:"minute"`
}

// Kind implements river.JobArgs.
func (RollupTickArgs) Kind() string { return kindRollupTick }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (RollupTickArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type tickWorker struct {
	river.WorkerDefaults[RollupTickArgs]
	owners   OwnerLister
	enqueuer Enqueuer
	now      func() time.Time
}

// Work implements river.Worker. An owner whose rollup cannot be queued does not
// stop the others; the failures are returned together so the tick is retried.
func (w *tickWorker) Work(ctx context.Context, _ *river.Job[RollupTickArgs]) error {
	list, err := w.owners.ActiveOwners(ctx)
	if err != nil {
		return fmt.Errorf("list owners: %w", err)
	}
	var errs []error
	for _, due := range owners.DueAt(list, w.now(), rollupHour, rollupMinute) {
		if err := w.enqueuer.EnqueueRollup(ctx, due.Owner.ID, due.Date); err != nil {
			errs = append(errs, fmt.Errorf("owner %s: %w", due.Owner.ID, err))
		}
	}
	return errors.Join(errs...)
}

type rollupWorker struct {
	river.WorkerDefaults[RollupArgs]
	roller Roller
	log    *slog.Logger
}

// Work implements river.Worker. The rollup is idempotent, so a retry is safe. An
// owner id that is not one can never succeed, so the job is cancelled.
func (w *rollupWorker) Work(ctx context.Context, job *river.Job[RollupArgs]) error {
	owner, err := uuid.Parse(job.Args.OwnerID)
	if err != nil {
		return river.JobCancel(fmt.Errorf("rollup owner %q is not a uuid: %w", job.Args.OwnerID, err))
	}
	if err := w.roller.Rollup(ctx, owner); err != nil {
		return fmt.Errorf("rollup for %s: %w", job.Args.Date, err)
	}
	w.log.Info("rollup done", slog.String("owner_id", owner.String()), slog.String("date", job.Args.Date))
	return nil
}

// Setup is what the outbox relay needs to run sensei's scheduled job on its
// River client: pass the fields to relay.Config.
type Setup struct {
	Workers      func(*river.Workers)
	Queues       map[string]river.QueueConfig
	PeriodicJobs []*river.PeriodicJob
}

func tickArgs(now func() time.Time) func() (river.JobArgs, *river.InsertOpts) {
	return func() (river.JobArgs, *river.InsertOpts) {
		return RollupTickArgs{Minute: now().UTC().Format(minuteLayout)}, nil
	}
}

// NewSetup builds the scheduled job. now is the clock, injected for tests.
func NewSetup(roller Roller, lister OwnerLister, enqueuer Enqueuer, now func() time.Time, log *slog.Logger) Setup {
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &tickWorker{owners: lister, enqueuer: enqueuer, now: now})
			river.AddWorker(ws, &rollupWorker{roller: roller, log: log})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: 1}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(tickEvery), tickArgs(now), nil),
		},
	}
}

// ContextEnqueuer queues rollups on the River client the calling worker runs
// on, so it can only be used from inside a worker.
type ContextEnqueuer struct{}

// EnqueueRollup implements Enqueuer.
func (ContextEnqueuer) EnqueueRollup(ctx context.Context, owner uuid.UUID, date string) error {
	client := river.ClientFromContext[pgx.Tx](ctx)
	if _, err := client.Insert(ctx, RollupArgs{OwnerID: owner.String(), Date: date}, nil); err != nil {
		return fmt.Errorf("enqueue rollup: %w", err)
	}
	return nil
}
