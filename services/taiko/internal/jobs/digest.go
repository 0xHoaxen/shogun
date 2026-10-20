// Package jobs holds taiko's River workers: the daily digest.
//
// The digest shares the outbox relay's River client (see relay.Config), so it
// is handed over as a Setup rather than started here.
//
// Each owner's digest goes out at digestHour:digestMinute on their own clock. A
// tick every tickEvery asks who has reached that time and queues one
// daily_digest job per owner and local date, unique by args, so the digest is
// written once per owner per day however often the tick runs.
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
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
)

const (
	// Queue is the River queue of taiko's own jobs.
	Queue = "taiko"

	kindDailyDigest = "daily_digest"
	kindDigestTick  = "digest_tick"

	digestHour, digestMinute = 8, 30

	// tickEvery is how often taiko looks for owners whose digest time has come.
	tickEvery = 15 * time.Minute

	minuteLayout = "2006-01-02T15:04"
)

// Digester writes one owner's daily digest. It returns how long to wait when
// quiet hours hold the digest back.
type Digester interface {
	RunFor(ctx context.Context, owner uuid.UUID, date string) (time.Duration, error)
}

// OwnerLister lists the owners whose digest may be due.
type OwnerLister interface {
	ActiveOwners(ctx context.Context) ([]owners.Owner, error)
}

// Enqueuer queues the digest of one owner for one local date.
type Enqueuer interface {
	EnqueueDigest(ctx context.Context, owner uuid.UUID, date string) error
}

// DailyDigestArgs are the args of daily_digest. Date is the owner's local
// calendar date, YYYY-MM-DD; unique-by-args makes the job run once per owner
// and date.
type DailyDigestArgs struct {
	OwnerID string `json:"owner_id"`
	Date    string `json:"date"`
}

// Kind implements river.JobArgs.
func (DailyDigestArgs) Kind() string { return kindDailyDigest }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (DailyDigestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// DigestTickArgs are the args of digest_tick; Minute is the UTC minute it was
// queued for, so a tick is queued once however many replicas run.
type DigestTickArgs struct {
	Minute string `json:"minute"`
}

// Kind implements river.JobArgs.
func (DigestTickArgs) Kind() string { return kindDigestTick }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (DigestTickArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type tickWorker struct {
	river.WorkerDefaults[DigestTickArgs]
	owners   OwnerLister
	enqueuer Enqueuer
	now      func() time.Time
	log      *slog.Logger
}

// Work implements river.Worker. An owner whose digest cannot be queued does not
// stop the others; the failures are returned together so the tick is retried.
func (w *tickWorker) Work(ctx context.Context, _ *river.Job[DigestTickArgs]) error {
	list, err := w.owners.ActiveOwners(ctx)
	if err != nil {
		return fmt.Errorf("list owners: %w", err)
	}
	var errs []error
	for _, due := range owners.DueAt(list, w.now(), digestHour, digestMinute) {
		if err := w.enqueuer.EnqueueDigest(ctx, due.Owner.ID, due.Date); err != nil {
			errs = append(errs, fmt.Errorf("owner %s: %w", due.Owner.ID, err))
		}
	}
	return errors.Join(errs...)
}

type digestWorker struct {
	river.WorkerDefaults[DailyDigestArgs]
	digester Digester
	log      *slog.Logger
}

// Work implements river.Worker. An owner id or date that is not one can never
// succeed, so the job is cancelled; quiet hours snooze the job until they end.
func (w *digestWorker) Work(ctx context.Context, job *river.Job[DailyDigestArgs]) error {
	owner, err := uuid.Parse(job.Args.OwnerID)
	if err != nil {
		return river.JobCancel(fmt.Errorf("digest owner %q is not a uuid: %w", job.Args.OwnerID, err))
	}
	wait, err := w.digester.RunFor(ctx, owner, job.Args.Date)
	if errors.Is(err, app.ErrBadDigestDate) {
		return river.JobCancel(err)
	}
	if err != nil {
		return fmt.Errorf("daily digest for %s: %w", job.Args.Date, err)
	}
	if wait > 0 {
		w.log.Info("daily digest held back by quiet hours",
			slog.String("date", job.Args.Date), slog.Duration("wait", wait))
		return river.JobSnooze(wait)
	}
	w.log.Info("daily digest done", slog.String("date", job.Args.Date))
	return nil
}

// Setup is what the outbox relay needs to run taiko's scheduled jobs on its
// River client: pass the fields to relay.Config.
type Setup struct {
	Workers      func(*river.Workers)
	Queues       map[string]river.QueueConfig
	PeriodicJobs []*river.PeriodicJob
}

func tickArgs(now func() time.Time) func() (river.JobArgs, *river.InsertOpts) {
	return func() (river.JobArgs, *river.InsertOpts) {
		return DigestTickArgs{Minute: now().UTC().Format(minuteLayout)}, nil
	}
}

// NewSetup builds the scheduled jobs. now is the clock, injected for tests.
func NewSetup(digester Digester, lister OwnerLister, enqueuer Enqueuer, now func() time.Time, log *slog.Logger) Setup {
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &tickWorker{owners: lister, enqueuer: enqueuer, now: now, log: log})
			river.AddWorker(ws, &digestWorker{digester: digester, log: log})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: 1}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(tickEvery), tickArgs(now), nil),
		},
	}
}

// ContextEnqueuer queues digests on the River client the calling worker runs
// on, so it can only be used from inside a worker.
type ContextEnqueuer struct{}

// EnqueueDigest implements Enqueuer.
func (ContextEnqueuer) EnqueueDigest(ctx context.Context, owner uuid.UUID, date string) error {
	client := river.ClientFromContext[pgx.Tx](ctx)
	if _, err := client.Insert(ctx, DailyDigestArgs{OwnerID: owner.String(), Date: date}, nil); err != nil {
		return fmt.Errorf("enqueue daily_digest: %w", err)
	}
	return nil
}
