package jobs

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type fakeSyncer struct {
	err   error
	calls int
}

func (f *fakeSyncer) SyncAll(context.Context) error {
	f.calls++
	return f.err
}

func TestWorkSyncsEveryAccountOnce(t *testing.T) {
	s := &fakeSyncer{}
	w := &syncWorker{syncer: s, log: slog.New(slog.DiscardHandler)}

	err := w.Work(context.Background(), &river.Job[GmailSyncArgs]{JobRow: &rivertype.JobRow{}})

	if err != nil || s.calls != 1 {
		t.Fatalf("err %v, calls %d", err, s.calls)
	}
}

func TestWorkReportsAFailedSync(t *testing.T) {
	boom := errors.New("1 of 2 accounts failed")
	w := &syncWorker{syncer: &fakeSyncer{err: boom}, log: slog.New(slog.DiscardHandler)}

	err := w.Work(context.Background(), &river.Job[GmailSyncArgs]{JobRow: &rivertype.JobRow{}})

	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestSetupSchedulesTheSyncEveryFiveMinutesWithoutOverlap(t *testing.T) {
	setup := NewSetup(&fakeSyncer{}, &fakeClassifier{}, slog.New(slog.DiscardHandler))

	opts := GmailSyncArgs{}.InsertOpts()

	if setup.Workers == nil || len(setup.PeriodicJobs) != 1 || setup.Queues[Queue].MaxWorkers < 1 {
		t.Fatalf("got %+v", setup)
	}
	if opts.Queue != Queue || opts.MaxAttempts != 1 || !opts.UniqueOpts.ByArgs || !slices.Equal(opts.UniqueOpts.ByState, activeStates) {
		t.Fatalf("insert opts %+v: a sync must be unique while one waits or runs, and never retry", opts)
	}
	if got := (&syncWorker{}).Timeout(nil); got != syncTimeout || syncTimeout >= syncEvery {
		t.Fatalf("timeout %s must be under the %s interval", got, syncEvery)
	}
}
