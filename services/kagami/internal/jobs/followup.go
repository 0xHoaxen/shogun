// Package jobs holds the kagami River workers: the daily follow-up scan.
//
// The scan shares the outbox relay's River client (see relay.Config), so it is
// handed over as a Setup rather than started here.
//
// Each owner is scanned at scanHour:scanMinute on their own clock. A tick every
// tickEvery asks who has reached that time and queues one daily_scan job per
// owner and local date, unique by args, so an owner is scanned once a day
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
	// Queue is the River queue of kagami's own jobs.
	Queue = "kagami"

	kindDailyScan = "daily_scan"
	kindScanTick  = "daily_scan_tick"

	// The scan runs at 07:55 so the follow-ups its stale step plans for today are
	// emitted by its own due step straight after.
	scanHour, scanMinute = 7, 55

	// tickEvery is how often kagami looks for owners whose scan time has come.
	tickEvery = 15 * time.Minute

	minuteLayout = "2006-01-02T15:04"
)

// Scanner is what the scan needs from the use cases. Both calls act on one
// owner and a day in that owner's zone.
type Scanner interface {
	ScanStaleApplications(ctx context.Context, owner uuid.UUID, date time.Time) (int, error)
	ScanDueFollowUps(ctx context.Context, owner uuid.UUID, date time.Time) (int, error)
}

// OwnerLister lists the owners whose scan may be due.
type OwnerLister interface {
	ActiveOwners(ctx context.Context) ([]owners.Owner, error)
}

// Enqueuer queues the scan of one owner for one local date.
type Enqueuer interface {
	EnqueueScan(ctx context.Context, owner uuid.UUID, date string) error
}

// DailyScanArgs are the args of daily_scan. Date is the owner's local calendar
// date, YYYY-MM-DD; with unique-by-args it makes the job run once per owner and
// date.
type DailyScanArgs struct {
	OwnerID string `json:"owner_id"`
	Date    string `json:"date"`
}

// Kind implements river.JobArgs.
func (DailyScanArgs) Kind() string { return kindDailyScan }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (DailyScanArgs) InsertOpts() river.InsertOpts { return scanInsertOpts() }

// ScanTickArgs are the args of daily_scan_tick; Minute is the UTC minute it was
// queued for, so a tick is queued once however many replicas run.
type ScanTickArgs struct {
	Minute string `json:"minute"`
}

// Kind implements river.JobArgs.
func (ScanTickArgs) Kind() string { return kindScanTick }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (ScanTickArgs) InsertOpts() river.InsertOpts { return scanInsertOpts() }

func scanInsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type tickWorker struct {
	river.WorkerDefaults[ScanTickArgs]
	owners   OwnerLister
	enqueuer Enqueuer
	now      func() time.Time
}

// Work implements river.Worker. An owner whose scan cannot be queued does not
// stop the others; the failures are returned together so the tick is retried.
func (w *tickWorker) Work(ctx context.Context, _ *river.Job[ScanTickArgs]) error {
	list, err := w.owners.ActiveOwners(ctx)
	if err != nil {
		return fmt.Errorf("list owners: %w", err)
	}
	var errs []error
	for _, due := range owners.DueAt(list, w.now(), scanHour, scanMinute) {
		if err := w.enqueuer.EnqueueScan(ctx, due.Owner.ID, due.Date); err != nil {
			errs = append(errs, fmt.Errorf("owner %s: %w", due.Owner.ID, err))
		}
	}
	return errors.Join(errs...)
}

type scanWorker struct {
	river.WorkerDefaults[DailyScanArgs]
	scanner Scanner
	log     *slog.Logger
}

// Work implements river.Worker. It first plans follow-ups for stale
// applications, then emits everything due that day, so a follow-up planned
// this morning is announced this morning. An owner id or date that is not one
// can never succeed, so the job is cancelled.
func (w *scanWorker) Work(ctx context.Context, job *river.Job[DailyScanArgs]) error {
	owner, err := uuid.Parse(job.Args.OwnerID)
	if err != nil {
		return river.JobCancel(fmt.Errorf("scan owner %q is not a uuid: %w", job.Args.OwnerID, err))
	}
	date, err := parseScanDate(job.Args.Date)
	if err != nil {
		return river.JobCancel(err)
	}
	planned, err := w.scanner.ScanStaleApplications(ctx, owner, date)
	if err != nil {
		return fmt.Errorf("stale application scan for %s: %w", job.Args.Date, err)
	}
	emitted, err := w.scanner.ScanDueFollowUps(ctx, owner, date)
	if err != nil {
		return fmt.Errorf("follow-up scan for %s: %w", job.Args.Date, err)
	}
	w.log.Info("daily scan done", slog.String("date", job.Args.Date),
		slog.Int("planned", planned), slog.Int("events", emitted))
	return nil
}

func parseScanDate(value string) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("scan date %q is not YYYY-MM-DD: %w", value, err)
	}
	return date, nil
}

// Setup is what the outbox relay needs to run kagami's scheduled jobs on its
// River client: pass the fields to relay.Config.
type Setup struct {
	Workers      func(*river.Workers)
	Queues       map[string]river.QueueConfig
	PeriodicJobs []*river.PeriodicJob
}

func tickArgs(now func() time.Time) func() (river.JobArgs, *river.InsertOpts) {
	return func() (river.JobArgs, *river.InsertOpts) {
		return ScanTickArgs{Minute: now().UTC().Format(minuteLayout)}, nil
	}
}

// NewSetup builds the scheduled jobs. now is the clock, injected for tests.
func NewSetup(scanner Scanner, lister OwnerLister, enqueuer Enqueuer, now func() time.Time, log *slog.Logger) Setup {
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &tickWorker{owners: lister, enqueuer: enqueuer, now: now})
			river.AddWorker(ws, &scanWorker{scanner: scanner, log: log})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: 1}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(tickEvery), tickArgs(now), nil),
		},
	}
}

// ContextEnqueuer queues scans on the River client the calling worker runs on,
// so it can only be used from inside a worker.
type ContextEnqueuer struct{}

// EnqueueScan implements Enqueuer.
func (ContextEnqueuer) EnqueueScan(ctx context.Context, owner uuid.UUID, date string) error {
	client := river.ClientFromContext[pgx.Tx](ctx)
	if _, err := client.Insert(ctx, DailyScanArgs{OwnerID: owner.String(), Date: date}, nil); err != nil {
		return fmt.Errorf("enqueue daily_scan: %w", err)
	}
	return nil
}
