// Package jobs holds katana's River workers: the daily GitHub sync and the
// suggestion runs.
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
	"github.com/riverqueue/river"

	"github.com/0xHoaxen/shogun/pkg/owners"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
)

const (
	// Queue is the River queue of katana's own jobs.
	Queue = "katana"

	kindGitHubSync = "github_sync"
	kindSyncTick   = "github_sync_tick"

	syncHour, syncMinute = 2, 0
	syncMaxAttempts      = 3

	// tickEvery is how often katana looks for owners whose sync time has come.
	tickEvery = 15 * time.Minute

	minuteLayout = "2006-01-02T15:04"
)

// Syncer reads GitHub for one owner.
type Syncer interface {
	SyncFor(ctx context.Context, owner uuid.UUID) (app.SyncResult, error)
}

// OwnerLister lists the owners whose GitHub may be synced: active owners with a
// GitHub connection.
type OwnerLister interface {
	SyncableOwners(ctx context.Context) ([]owners.Owner, error)
}

// Enqueuer queues the sync of one owner for one local date.
type Enqueuer interface {
	EnqueueSync(ctx context.Context, owner uuid.UUID, date string) error
}

// GitHubSyncArgs are the args of github_sync. Date is the owner's local calendar
// date, YYYY-MM-DD; unique-by-args makes the job run once per owner and date.
type GitHubSyncArgs struct {
	OwnerID string `json:"owner_id"`
	Date    string `json:"date"`
}

// Kind implements river.JobArgs.
func (GitHubSyncArgs) Kind() string { return kindGitHubSync }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (GitHubSyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: syncMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// SyncTickArgs are the args of github_sync_tick; Minute is the UTC minute it
// was queued for, so a tick is queued once however many replicas run.
type SyncTickArgs struct {
	Minute string `json:"minute"`
}

// Kind implements river.JobArgs.
func (SyncTickArgs) Kind() string { return kindSyncTick }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (SyncTickArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type tickWorker struct {
	river.WorkerDefaults[SyncTickArgs]
	owners   OwnerLister
	enqueuer Enqueuer
	now      func() time.Time
}

// Work implements river.Worker. An owner whose sync cannot be queued does not
// stop the others; the failures are returned together so the tick is retried.
func (w *tickWorker) Work(ctx context.Context, _ *river.Job[SyncTickArgs]) error {
	list, err := w.owners.SyncableOwners(ctx)
	if err != nil {
		return fmt.Errorf("list owners: %w", err)
	}
	var errs []error
	for _, due := range owners.DueAt(list, w.now(), syncHour, syncMinute) {
		if err := w.enqueuer.EnqueueSync(ctx, due.Owner.ID, due.Date); err != nil {
			errs = append(errs, fmt.Errorf("owner %s: %w", due.Owner.ID, err))
		}
	}
	return errors.Join(errs...)
}

type syncWorker struct {
	river.WorkerDefaults[GitHubSyncArgs]
	syncer Syncer
	log    *slog.Logger
}

// Work implements river.Worker. An owner with no GitHub connection has nothing
// to sync, which is logged and not retried; an owner id that is not one can
// never succeed; a rate limit snoozes the job until GitHub lifts it.
func (w *syncWorker) Work(ctx context.Context, job *river.Job[GitHubSyncArgs]) error {
	owner, err := uuid.Parse(job.Args.OwnerID)
	if err != nil {
		return river.JobCancel(fmt.Errorf("sync owner %q is not a uuid: %w", job.Args.OwnerID, err))
	}
	res, err := w.syncer.SyncFor(ctx, owner)
	var limit *app.RateLimitError
	switch {
	case err == nil:
		w.log.Info("github sync done", slog.String("owner_id", owner.String()), slog.Bool("changed", res.Changed))
		return nil
	case errors.Is(err, app.ErrGitHubNotConfigured), errors.Is(err, app.ErrGitHubNotConnected):
		w.log.Warn("github sync skipped: no github connection", slog.String("owner_id", owner.String()), slog.String("date", job.Args.Date))
		return nil
	case errors.As(err, &limit):
		wait := time.Until(limit.ResetAt)
		w.log.Info("github sync held back by the rate limit", slog.String("date", job.Args.Date), slog.Duration("wait", wait))
		return river.JobSnooze(max(wait, time.Minute))
	default:
		return fmt.Errorf("github sync for %s: %w", job.Args.Date, err)
	}
}

// Setup is what the outbox relay needs to run katana's scheduled jobs on its
// River client: pass the fields to relay.Config.
type Setup struct {
	Workers      func(*river.Workers)
	Queues       map[string]river.QueueConfig
	PeriodicJobs []*river.PeriodicJob
}

func tickArgs(now func() time.Time) func() (river.JobArgs, *river.InsertOpts) {
	return func() (river.JobArgs, *river.InsertOpts) {
		return SyncTickArgs{Minute: now().UTC().Format(minuteLayout)}, nil
	}
}

// NewSetup builds the scheduled jobs. now is the clock, injected for tests.
func NewSetup(syncer Syncer, suggester Suggester, lister OwnerLister, enqueuer Enqueuer, now func() time.Time, log *slog.Logger) Setup {
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &tickWorker{owners: lister, enqueuer: enqueuer, now: now})
			river.AddWorker(ws, &syncWorker{syncer: syncer, log: log})
			river.AddWorker(ws, &suggestWorker{suggester: suggester, now: now, log: log})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: 1}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(tickEvery), tickArgs(now), nil),
		},
	}
}

// ContextEnqueuer queues syncs on the River client the calling worker runs on,
// so it can only be used from inside a worker.
type ContextEnqueuer struct{}

// EnqueueSync implements Enqueuer.
func (ContextEnqueuer) EnqueueSync(ctx context.Context, owner uuid.UUID, date string) error {
	client := river.ClientFromContext[pgx.Tx](ctx)
	if _, err := client.Insert(ctx, GitHubSyncArgs{OwnerID: owner.String(), Date: date}, nil); err != nil {
		return fmt.Errorf("enqueue github_sync: %w", err)
	}
	return nil
}
