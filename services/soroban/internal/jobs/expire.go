// Package jobs holds the soroban River workers: the sweeper that gives back
// reservations nobody committed.
//
// The sweeper shares the outbox relay's River client (see relay.Config), so it
// is handed over as a Setup rather than started here.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

const (
	// Queue is the River queue of soroban's own jobs.
	Queue = "soroban"

	kindExpireReservations = "expire_reservations"

	// expireEvery is how often the sweeper looks for expired reservations.
	expireEvery = time.Minute
)

// Expirer is what the sweeper needs from the use cases.
type Expirer interface {
	ExpireReservations(ctx context.Context) (int, error)
}

// ExpireReservationsArgs are the args of expire_reservations; it has none.
type ExpireReservationsArgs struct{}

// Kind implements river.JobArgs.
func (ExpireReservationsArgs) Kind() string { return kindExpireReservations }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (ExpireReservationsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue}
}

type expireWorker struct {
	river.WorkerDefaults[ExpireReservationsArgs]
	expirer Expirer
	log     *slog.Logger
}

func (w *expireWorker) Work(ctx context.Context, _ *river.Job[ExpireReservationsArgs]) error {
	expired, err := w.expirer.ExpireReservations(ctx)
	if err != nil {
		return fmt.Errorf("expire reservations: %w", err)
	}
	if expired > 0 {
		w.log.Info("reservations expired", slog.Int("count", expired))
	}
	return nil
}

// Setup is what the outbox relay needs to run soroban's scheduled jobs on its
// River client: pass the fields to relay.Config.
type Setup struct {
	Workers      func(*river.Workers)
	Queues       map[string]river.QueueConfig
	PeriodicJobs []*river.PeriodicJob
}

// NewSetup builds the scheduled jobs.
func NewSetup(expirer Expirer, log *slog.Logger) Setup {
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &expireWorker{expirer: expirer, log: log})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: 1}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(expireEvery), func() (river.JobArgs, *river.InsertOpts) {
				return ExpireReservationsArgs{}, nil
			}, nil),
		},
	}
}
