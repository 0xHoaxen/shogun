// Package jobs holds sensei's River worker: the nightly rollup.
//
// The rollup shares the outbox relay's River client (see relay.Config), so it is
// handed over as a Setup rather than started here.
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
	// Queue is the River queue of sensei's own jobs.
	Queue = "sensei"

	// locationName is the zone the owner's days are counted in.
	locationName = "Asia/Kolkata"

	kindRollup = "rollup"

	rollupHour, rollupMinute = 1, 0
	rollupMaxAttempts        = 3
)

// Roller rebuilds the daily rollups.
type Roller interface {
	Rollup(ctx context.Context) error
}

// RollupArgs are the args of rollup. Date is the local calendar date,
// YYYY-MM-DD; unique-by-args makes the job run once per date.
type RollupArgs struct {
	Date string `json:"date"`
}

// Kind implements river.JobArgs.
func (RollupArgs) Kind() string { return kindRollup }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (RollupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: rollupMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type rollupWorker struct {
	river.WorkerDefaults[RollupArgs]
	roller Roller
	log    *slog.Logger
}

// Work implements river.Worker. The rollup is idempotent, so a retry is safe.
func (w *rollupWorker) Work(ctx context.Context, job *river.Job[RollupArgs]) error {
	if err := w.roller.Rollup(ctx); err != nil {
		return fmt.Errorf("rollup for %s: %w", job.Args.Date, err)
	}
	w.log.Info("rollup done", slog.String("date", job.Args.Date))
	return nil
}

// Setup is what the outbox relay needs to run sensei's scheduled job on its
// River client: pass the fields to relay.Config.
type Setup struct {
	Workers      func(*river.Workers)
	Queues       map[string]river.QueueConfig
	PeriodicJobs []*river.PeriodicJob
}

// Location loads the zone the owner's days are counted in.
func Location() (*time.Location, error) {
	loc, err := time.LoadLocation(locationName)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", locationName, err)
	}
	return loc, nil
}

func rollupArgs(now func() time.Time, loc *time.Location) func() (river.JobArgs, *river.InsertOpts) {
	return func() (river.JobArgs, *river.InsertOpts) {
		return RollupArgs{Date: now().In(loc).Format(time.DateOnly)}, nil
	}
}

// NewSetup builds the scheduled job. now is the clock, injected for tests.
func NewSetup(roller Roller, loc *time.Location, now func() time.Time, log *slog.Logger) Setup {
	at := schedule.Daily{Hour: rollupHour, Minute: rollupMinute, Loc: loc}
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &rollupWorker{roller: roller, log: log})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: 1}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(at, rollupArgs(now, loc), nil),
		},
	}
}
