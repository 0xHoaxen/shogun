// Package jobs holds taiko's River workers: the daily digest.
//
// The digest shares the outbox relay's River client (see relay.Config), so it
// is handed over as a Setup rather than started here.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/0xHoaxen/shogun/pkg/schedule"
)

const (
	// Queue is the River queue of taiko's own jobs.
	Queue = "taiko"

	// digestLocation is the zone the owner's days are counted in.
	digestLocation = "Asia/Kolkata"

	kindDailyDigest = "daily_digest"

	digestHour, digestMinute = 8, 30
)

// Digester writes the daily digest. It returns how long to wait when quiet
// hours hold the digest back.
type Digester interface {
	Run(ctx context.Context, date time.Time) (time.Duration, error)
}

// DailyDigestArgs are the args of daily_digest. Date is the local calendar
// date, YYYY-MM-DD; unique-by-args makes the job run once per date.
type DailyDigestArgs struct {
	Date string `json:"date"`
}

// Kind implements river.JobArgs.
func (DailyDigestArgs) Kind() string { return kindDailyDigest }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (DailyDigestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type digestWorker struct {
	river.WorkerDefaults[DailyDigestArgs]
	digester Digester
	loc      *time.Location
	log      *slog.Logger
}

// Work implements river.Worker. A date that is not a date can never succeed, so
// it is cancelled; quiet hours snooze the job until they end.
func (w *digestWorker) Work(ctx context.Context, job *river.Job[DailyDigestArgs]) error {
	date, err := time.ParseInLocation(time.DateOnly, job.Args.Date, w.loc)
	if err != nil {
		return river.JobCancel(fmt.Errorf("digest date %q is not YYYY-MM-DD: %w", job.Args.Date, err))
	}
	wait, err := w.digester.Run(ctx, date)
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

// Location loads the zone the owner's days are counted in.
func Location() (*time.Location, error) {
	loc, err := time.LoadLocation(digestLocation)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", digestLocation, err)
	}
	return loc, nil
}

// digestArgs builds the args of each scheduled run: today's date in the
// owner's zone, so the job runs once per day however often it is retried.
func digestArgs(now func() time.Time, loc *time.Location) func() (river.JobArgs, *river.InsertOpts) {
	return func() (river.JobArgs, *river.InsertOpts) {
		return DailyDigestArgs{Date: now().In(loc).Format(time.DateOnly)}, nil
	}
}

// NewSetup builds the scheduled jobs. now is the clock, injected for tests.
func NewSetup(digester Digester, loc *time.Location, now func() time.Time, log *slog.Logger) Setup {
	at := schedule.Daily{Hour: digestHour, Minute: digestMinute, Loc: loc}
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &digestWorker{digester: digester, loc: loc, log: log})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: 1}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(at, digestArgs(now, loc), nil),
		},
	}
}
