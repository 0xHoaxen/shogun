package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
)

const (
	kindReconcileSends = "reconcile_sends"

	// reconcileEvery is how often unfinished sends are looked at.
	reconcileEvery   = 2 * time.Minute
	reconcileTimeout = time.Minute
)

// Reconciler is what the job needs from the send use case.
type Reconciler interface {
	Reconcile(ctx context.Context) (app.ReconcileResult, error)
}

// ReconcileSendsArgs are the args of reconcile_sends; it has none.
type ReconcileSendsArgs struct{}

// Kind implements river.JobArgs.
func (ReconcileSendsArgs) Kind() string { return kindReconcileSends }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (ReconcileSendsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       Queue,
		MaxAttempts: 1, // the next run is two minutes away
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByState: activeStates},
	}
}

type reconcileWorker struct {
	river.WorkerDefaults[ReconcileSendsArgs]
	reconciler Reconciler
	log        *slog.Logger
}

func (w *reconcileWorker) Timeout(*river.Job[ReconcileSendsArgs]) time.Duration {
	return reconcileTimeout
}

// Work resolves sends left unfinished. It only reads the provider's Sent mail
// and records outcomes; it never sends.
func (w *reconcileWorker) Work(ctx context.Context, _ *river.Job[ReconcileSendsArgs]) error {
	res, err := w.reconciler.Reconcile(ctx)
	if err != nil {
		return fmt.Errorf("reconcile sends: %w", err)
	}
	if res.Recovered > 0 || res.Failed > 0 {
		w.log.Info("sends reconciled", slog.Int("recovered", res.Recovered), slog.Int("failed", res.Failed))
	}
	return nil
}
