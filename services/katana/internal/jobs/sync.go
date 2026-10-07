// Package jobs holds katana's River workers: the daily GitHub sync.
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

	"github.com/riverqueue/river"

	"github.com/0xHoaxen/shogun/pkg/schedule"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
)

const (
	// Queue is the River queue of katana's own jobs.
	Queue = "katana"

	// syncLocation is the zone the owner's days are counted in.
	syncLocation = "Asia/Kolkata"

	kindGitHubSync = "github_sync"

	syncHour, syncMinute = 2, 0
	syncMaxAttempts      = 3
)

// Syncer reads GitHub for every owner katana knows.
type Syncer interface {
	SyncAll(ctx context.Context, log *slog.Logger) error
}

// GitHubSyncArgs are the args of github_sync. Date is the local calendar date,
// YYYY-MM-DD; unique-by-args makes the job run once per date.
type GitHubSyncArgs struct {
	Date string `json:"date"`
}

// Kind implements river.JobArgs.
func (GitHubSyncArgs) Kind() string { return kindGitHubSync }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (GitHubSyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: syncMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type syncWorker struct {
	river.WorkerDefaults[GitHubSyncArgs]
	syncer Syncer
	log    *slog.Logger
}

// Work implements river.Worker. Without a GitHub user and token there is
// nothing to do, which is logged and not retried; a rate limit snoozes the job
// until GitHub lifts it.
func (w *syncWorker) Work(ctx context.Context, job *river.Job[GitHubSyncArgs]) error {
	err := w.syncer.SyncAll(ctx, w.log)
	var limit *app.RateLimitError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, app.ErrGitHubNotConfigured):
		w.log.Warn("github sync skipped: no github user or token configured", slog.String("date", job.Args.Date))
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

// Location loads the zone the owner's days are counted in.
func Location() (*time.Location, error) {
	loc, err := time.LoadLocation(syncLocation)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", syncLocation, err)
	}
	return loc, nil
}

// syncArgs builds the args of each scheduled run: today's date in the owner's
// zone, so the job runs once a day however often it is retried.
func syncArgs(now func() time.Time, loc *time.Location) func() (river.JobArgs, *river.InsertOpts) {
	return func() (river.JobArgs, *river.InsertOpts) {
		return GitHubSyncArgs{Date: now().In(loc).Format(time.DateOnly)}, nil
	}
}

// NewSetup builds the scheduled jobs. now is the clock, injected for tests.
func NewSetup(syncer Syncer, loc *time.Location, now func() time.Time, log *slog.Logger) Setup {
	at := schedule.Daily{Hour: syncHour, Minute: syncMinute, Loc: loc}
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &syncWorker{syncer: syncer, log: log})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: 1}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(at, syncArgs(now, loc), nil),
		},
	}
}
