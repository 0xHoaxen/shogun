package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/0xHoaxen/shogun/services/katana/internal/app"
)

type fakeSyncer struct {
	err   error
	calls int
}

func (f *fakeSyncer) SyncAll(context.Context, *slog.Logger) error {
	f.calls++
	return f.err
}

func work(t *testing.T, syncer Syncer) error {
	t.Helper()
	w := &syncWorker{syncer: syncer, log: slog.New(slog.DiscardHandler)}
	return w.Work(context.Background(), &river.Job[GitHubSyncArgs]{JobRow: &rivertype.JobRow{}, Args: GitHubSyncArgs{Date: "2026-10-08"}})
}

func TestWorkerSucceedsWhenTheSyncDoes(t *testing.T) {
	syncer := &fakeSyncer{}

	err := work(t, syncer)

	if err != nil || syncer.calls != 1 {
		t.Fatalf("err %v, calls %d", err, syncer.calls)
	}
}

func TestWorkerSkipsQuietlyWhenGitHubIsNotConfigured(t *testing.T) {
	err := work(t, &fakeSyncer{err: app.ErrGitHubNotConfigured})
	if err != nil {
		t.Fatalf("err = %v, want none and no retry", err)
	}
}

func TestWorkerSnoozesUntilTheRateLimitLifts(t *testing.T) {
	cause := errors.Join(errors.New("sync owner x"), &app.RateLimitError{ResetAt: time.Now().Add(20 * time.Minute)})

	err := work(t, &fakeSyncer{err: cause})

	var snooze *river.JobSnoozeError
	if !errors.As(err, &snooze) || snooze.Duration < 19*time.Minute || snooze.Duration > 21*time.Minute {
		t.Fatalf("err = %v, want a snooze of about 20 minutes", err)
	}
}

func TestWorkerSnoozesAtLeastAMinuteWhenTheLimitHasAlreadyLifted(t *testing.T) {
	err := work(t, &fakeSyncer{err: &app.RateLimitError{ResetAt: time.Now().Add(-time.Hour)}})

	var snooze *river.JobSnoozeError
	if !errors.As(err, &snooze) || snooze.Duration < time.Minute {
		t.Fatalf("err = %v, want a snooze of at least a minute", err)
	}
}

func TestWorkerRetriesOtherFailures(t *testing.T) {
	boom := errors.New("github down")

	err := work(t, &fakeSyncer{err: boom})

	var snooze *river.JobSnoozeError
	if !errors.Is(err, boom) || errors.As(err, &snooze) {
		t.Fatalf("err = %v, want the failure returned for River to retry", err)
	}
}

func TestSetupSchedulesTheSyncAtTwoInTheNightInIST(t *testing.T) {
	loc, err := Location()
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) // 17:30 IST

	setup := NewSetup(&fakeSyncer{}, nil, loc, func() time.Time { return now }, slog.New(slog.DiscardHandler))

	if len(setup.PeriodicJobs) != 1 || setup.Queues[Queue].MaxWorkers != 1 {
		t.Fatalf("setup = %+v", setup)
	}
	args, _ := syncArgs(func() time.Time { return now }, loc)()
	if got := args.(GitHubSyncArgs).Date; got != "2026-10-07" {
		t.Fatalf("date = %q, want today in IST", got)
	}
}
