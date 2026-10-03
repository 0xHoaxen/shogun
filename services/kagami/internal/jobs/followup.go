// Package jobs holds the kagami River workers: the daily follow-up scans.
//
// The scans share the outbox relay's River client (see relay.Config), so they
// are handed over as a Setup rather than started here.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

const (
	// Queue is the River queue of kagami's own jobs.
	Queue = "kagami"

	// scanLocation is the zone the owner's days are counted in.
	scanLocation = "Asia/Kolkata"

	kindFollowUpScan = "follow_up_scan"
	kindStaleScan    = "stale_application_scan"
)

// The stale scan runs shortly before the due scan so that the follow-ups it
// plans for today are emitted by the same morning's due scan.
const (
	staleScanHour, staleScanMinute       = 7, 55
	followUpScanHour, followUpScanMinute = 8, 0
)

// Scanner is what the scans need from the use cases.
type Scanner interface {
	ScanDueFollowUps(ctx context.Context, date time.Time) (int, error)
	ScanStaleApplications(ctx context.Context, date time.Time) (int, error)
}

// FollowUpScanArgs are the args of follow_up_scan. Date is the local calendar
// date, YYYY-MM-DD; with unique-by-args it makes the job run once per date.
type FollowUpScanArgs struct {
	Date string `json:"date"`
}

// Kind implements river.JobArgs.
func (FollowUpScanArgs) Kind() string { return kindFollowUpScan }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (FollowUpScanArgs) InsertOpts() river.InsertOpts { return scanInsertOpts() }

// StaleApplicationScanArgs are the args of stale_application_scan.
type StaleApplicationScanArgs struct {
	Date string `json:"date"`
}

// Kind implements river.JobArgs.
func (StaleApplicationScanArgs) Kind() string { return kindStaleScan }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (StaleApplicationScanArgs) InsertOpts() river.InsertOpts { return scanInsertOpts() }

func scanInsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type followUpWorker struct {
	river.WorkerDefaults[FollowUpScanArgs]
	scanner Scanner
	log     *slog.Logger
}

func (w *followUpWorker) Work(ctx context.Context, job *river.Job[FollowUpScanArgs]) error {
	date, err := parseScanDate(job.Args.Date)
	if err != nil {
		return river.JobCancel(err)
	}
	emitted, err := w.scanner.ScanDueFollowUps(ctx, date)
	if err != nil {
		return fmt.Errorf("follow-up scan for %s: %w", job.Args.Date, err)
	}
	w.log.Info("follow-up scan done", slog.String("date", job.Args.Date), slog.Int("events", emitted))
	return nil
}

type staleWorker struct {
	river.WorkerDefaults[StaleApplicationScanArgs]
	scanner Scanner
	log     *slog.Logger
}

func (w *staleWorker) Work(ctx context.Context, job *river.Job[StaleApplicationScanArgs]) error {
	date, err := parseScanDate(job.Args.Date)
	if err != nil {
		return river.JobCancel(err)
	}
	planned, err := w.scanner.ScanStaleApplications(ctx, date)
	if err != nil {
		return fmt.Errorf("stale application scan for %s: %w", job.Args.Date, err)
	}
	w.log.Info("stale application scan done", slog.String("date", job.Args.Date), slog.Int("planned", planned))
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

// NewSetup builds the scheduled jobs. now is the clock, injected for tests.
func NewSetup(scanner Scanner, now func() time.Time, log *slog.Logger) (Setup, error) {
	loc, err := time.LoadLocation(scanLocation)
	if err != nil {
		return Setup{}, fmt.Errorf("load %s: %w", scanLocation, err)
	}
	staleScanAt := dailySchedule{hour: staleScanHour, minute: staleScanMinute, loc: loc}
	followUpScanAt := dailySchedule{hour: followUpScanHour, minute: followUpScanMinute, loc: loc}
	today := func() string { return scanDate(now(), loc) }

	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &staleWorker{scanner: scanner, log: log})
			river.AddWorker(ws, &followUpWorker{scanner: scanner, log: log})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: 1}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(staleScanAt, func() (river.JobArgs, *river.InsertOpts) {
				return StaleApplicationScanArgs{Date: today()}, nil
			}, nil),
			river.NewPeriodicJob(followUpScanAt, func() (river.JobArgs, *river.InsertOpts) {
				return FollowUpScanArgs{Date: today()}, nil
			}, nil),
		},
	}, nil
}
