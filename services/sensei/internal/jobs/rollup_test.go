package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type fakeRoller struct {
	err   error
	calls int
}

func (f *fakeRoller) Rollup(context.Context) error {
	f.calls++
	return f.err
}

func TestTheWorkerRollsUpAndReturnsAFailureForRetry(t *testing.T) {
	boom := errors.New("db down")
	for name, tt := range map[string]struct {
		roller  *fakeRoller
		wantErr bool
	}{"ok": {&fakeRoller{}, false}, "failure": {&fakeRoller{err: boom}, true}} {
		t.Run(name, func(t *testing.T) {
			w := &rollupWorker{roller: tt.roller, log: slog.New(slog.DiscardHandler)}

			err := w.Work(context.Background(), &river.Job[RollupArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: RollupArgs{Date: "2026-10-08"}})

			if (err != nil) != tt.wantErr || tt.roller.calls != 1 || (tt.wantErr && !errors.Is(err, boom)) {
				t.Fatalf("err %v, calls %d", err, tt.roller.calls)
			}
		})
	}
}

func TestSetupSchedulesTheRollupAtOneInTheNightInISTOncePerDate(t *testing.T) {
	loc, err := Location()
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	now := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC) // 01:30 IST on the 8th

	setup := NewSetup(&fakeRoller{}, loc, func() time.Time { return now }, slog.New(slog.DiscardHandler))
	args, _ := rollupArgs(func() time.Time { return now }, loc)()

	if len(setup.PeriodicJobs) != 1 || setup.Queues[Queue].MaxWorkers != 1 || loc.String() != "Asia/Kolkata" {
		t.Fatalf("setup = %+v", setup)
	}
	if got := args.(RollupArgs).Date; got != "2026-10-08" {
		t.Fatalf("date = %q, want the IST date", got)
	}
}
