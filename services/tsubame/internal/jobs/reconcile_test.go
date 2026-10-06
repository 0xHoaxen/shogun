package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
)

type fakeReconciler struct {
	res   app.ReconcileResult
	err   error
	calls int
}

func (f *fakeReconciler) Reconcile(context.Context) (app.ReconcileResult, error) {
	f.calls++
	return f.res, f.err
}

func TestReconcileWorkRunsTheReconciler(t *testing.T) {
	f := &fakeReconciler{res: app.ReconcileResult{Recovered: 2, Failed: 1}}
	w := &reconcileWorker{reconciler: f, log: slog.New(slog.DiscardHandler)}

	err := w.Work(context.Background(), &river.Job[ReconcileSendsArgs]{JobRow: &rivertype.JobRow{}})

	if err != nil || f.calls != 1 {
		t.Fatalf("err %v, calls %d", err, f.calls)
	}
}

func TestReconcileWorkReportsAFailure(t *testing.T) {
	boom := errors.New("db down")
	w := &reconcileWorker{reconciler: &fakeReconciler{err: boom}, log: slog.New(slog.DiscardHandler)}

	err := w.Work(context.Background(), &river.Job[ReconcileSendsArgs]{JobRow: &rivertype.JobRow{}})

	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestReconcileJobDoesNotOverlapOrRetry(t *testing.T) {
	opts := ReconcileSendsArgs{}.InsertOpts()

	if opts.Queue != Queue || opts.MaxAttempts != 1 || !opts.UniqueOpts.ByArgs || len(opts.UniqueOpts.ByState) != len(activeStates) {
		t.Fatalf("got %+v", opts)
	}
	if got := (&reconcileWorker{}).Timeout(nil); got != reconcileTimeout || reconcileTimeout >= reconcileEvery {
		t.Fatalf("timeout %s must be under the %s interval", got, reconcileEvery)
	}
}
