package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/riverqueue/river"
)

type fakeExpirer struct {
	expired int
	err     error
	calls   int
}

func (f *fakeExpirer) ExpireReservations(context.Context) (int, error) {
	f.calls++
	return f.expired, f.err
}

func TestNewSetupRegistersTheSweeperOnItsOwnQueue(t *testing.T) {
	setup := NewSetup(&fakeExpirer{}, slog.New(slog.DiscardHandler))

	if len(setup.PeriodicJobs) != 1 || setup.Queues[Queue].MaxWorkers != 1 || setup.Workers == nil {
		t.Fatalf("got %+v", setup)
	}
	if got := (ExpireReservationsArgs{}).InsertOpts().Queue; got != Queue {
		t.Fatalf("job queue = %q, want %q", got, Queue)
	}
}

func TestWorkerRunsTheSweepAndReportsItsFailure(t *testing.T) {
	tests := []struct {
		name    string
		expirer *fakeExpirer
		wantErr bool
	}{
		{"a sweep that expires some", &fakeExpirer{expired: 3}, false},
		{"a sweep that finds nothing", &fakeExpirer{}, false},
		{"a failing sweep is retried", &fakeExpirer{err: errors.New("db down")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &expireWorker{expirer: tt.expirer, log: slog.New(slog.DiscardHandler)}

			err := w.Work(context.Background(), &river.Job[ExpireReservationsArgs]{})

			if (err != nil) != tt.wantErr || tt.expirer.calls != 1 {
				t.Fatalf("err = %v, calls = %d", err, tt.expirer.calls)
			}
		})
	}
}
