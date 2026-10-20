package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/0xHoaxen/shogun/pkg/owners"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
)

type fakeSyncer struct {
	err   error
	calls int
	owner uuid.UUID
}

func (f *fakeSyncer) SyncFor(_ context.Context, owner uuid.UUID) (app.SyncResult, error) {
	f.calls++
	f.owner = owner
	return app.SyncResult{}, f.err
}

type fakeOwners struct {
	list []owners.Owner
	err  error
}

func (f fakeOwners) SyncableOwners(context.Context) ([]owners.Owner, error) { return f.list, f.err }

type queued struct {
	owner uuid.UUID
	date  string
}

type fakeEnqueuer struct {
	mu      sync.Mutex
	got     []queued
	failFor map[uuid.UUID]error
}

func (f *fakeEnqueuer) EnqueueSync(_ context.Context, owner uuid.UUID, date string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failFor[owner]; err != nil {
		return err
	}
	f.got = append(f.got, queued{owner, date})
	return nil
}

func work(t *testing.T, syncer Syncer) error {
	t.Helper()
	return workFor(t, syncer, uuid.NewString())
}

func workFor(t *testing.T, syncer Syncer, ownerID string) error {
	t.Helper()
	w := &syncWorker{syncer: syncer, log: slog.New(slog.DiscardHandler)}
	return w.Work(context.Background(), &river.Job[GitHubSyncArgs]{JobRow: &rivertype.JobRow{}, Args: GitHubSyncArgs{OwnerID: ownerID, Date: "2026-10-08"}})
}

func tick(list []owners.Owner, enq Enqueuer, now time.Time) error {
	w := &tickWorker{owners: fakeOwners{list: list}, enqueuer: enq, now: func() time.Time { return now }}
	return w.Work(context.Background(), &river.Job[SyncTickArgs]{})
}

func TestWorkerSyncsTheOwnerOfTheJob(t *testing.T) {
	syncer := &fakeSyncer{}
	owner := uuid.New()

	err := workFor(t, syncer, owner.String())

	if err != nil || syncer.calls != 1 || syncer.owner != owner {
		t.Fatalf("err %v, calls %d, owner %s", err, syncer.calls, syncer.owner)
	}
}

func TestWorkerSkipsQuietlyWhenThereIsNoGitHubConnection(t *testing.T) {
	for _, cause := range []error{app.ErrGitHubNotConfigured, app.ErrGitHubNotConnected} {
		if err := work(t, &fakeSyncer{err: cause}); err != nil {
			t.Fatalf("cause %v: err = %v, want none and no retry", cause, err)
		}
	}
}

func TestWorkerCancelsAnOwnerThatIsNotAUUID(t *testing.T) {
	syncer := &fakeSyncer{}

	err := workFor(t, syncer, "nope")

	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) || syncer.calls != 0 {
		t.Fatalf("err %v, calls %d; want the job cancelled before any work", err, syncer.calls)
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

func TestTickQueuesOneSyncPerOwnerWhoseClockHasReachedTwoInTheNight(t *testing.T) {
	// 21:00 UTC on the 6th is 02:30 on the 7th in Kolkata, 06:00 on the 7th in Tokyo and
	// 17:00 on the 6th in New York. All three are past 02:00 on their own day, each with its own date.
	now := time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)
	kolkata := owners.Owner{ID: uuid.New(), Timezone: "Asia/Kolkata"}
	tokyo := owners.Owner{ID: uuid.New(), Timezone: "Asia/Tokyo"}
	newYork := owners.Owner{ID: uuid.New(), Timezone: "America/New_York"}
	enq := &fakeEnqueuer{}

	err := tick([]owners.Owner{kolkata, tokyo, newYork}, enq, now)

	want := []queued{{kolkata.ID, "2026-10-07"}, {tokyo.ID, "2026-10-07"}, {newYork.ID, "2026-10-06"}}
	if err != nil || len(enq.got) != 3 || enq.got[0] != want[0] || enq.got[1] != want[1] || enq.got[2] != want[2] {
		t.Fatalf("err %v, queued %+v, want %+v", err, enq.got, want)
	}
}

func TestTickSkipsAnOwnerWhoseClockHasNotReachedTwo(t *testing.T) {
	// 19:00 UTC on the 6th is 00:30 on the 7th in Kolkata.
	now := time.Date(2026, 10, 6, 19, 0, 0, 0, time.UTC)
	enq := &fakeEnqueuer{}

	err := tick([]owners.Owner{{ID: uuid.New(), Timezone: "Asia/Kolkata"}}, enq, now)

	if err != nil || len(enq.got) != 0 {
		t.Fatalf("err %v, queued %+v, want none before 02:00 local", err, enq.got)
	}
}

func TestTickKeepsGoingPastAnOwnerItCannotQueueAndReportsIt(t *testing.T) {
	now := time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)
	broken := owners.Owner{ID: uuid.New(), Timezone: "Asia/Kolkata"}
	fine := owners.Owner{ID: uuid.New(), Timezone: "Asia/Kolkata"}
	boom := errors.New("queue is down")
	enq := &fakeEnqueuer{failFor: map[uuid.UUID]error{broken.ID: boom}}

	err := tick([]owners.Owner{broken, fine}, enq, now)

	if !errors.Is(err, boom) || len(enq.got) != 1 || enq.got[0].owner != fine.ID {
		t.Fatalf("err %v, queued %+v; want the failure reported and the other owner queued", err, enq.got)
	}
}

func TestTickReportsAnOwnerListThatCannotBeRead(t *testing.T) {
	boom := errors.New("database is down")
	w := &tickWorker{owners: fakeOwners{err: boom}, enqueuer: &fakeEnqueuer{}, now: time.Now}

	err := w.Work(context.Background(), &river.Job[SyncTickArgs]{})

	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the failure", err)
	}
}

func TestSetupTicksEveryFifteenMinutesAndEveryJobIsUniqueOnTheKatanaQueue(t *testing.T) {
	setup := NewSetup(&fakeSyncer{}, nil, fakeOwners{}, &fakeEnqueuer{}, time.Now, slog.New(slog.DiscardHandler))

	if len(setup.PeriodicJobs) != 1 || setup.Queues[Queue].MaxWorkers != 1 {
		t.Fatalf("setup = %+v", setup)
	}
	syncOpts, tickOpts := GitHubSyncArgs{}.InsertOpts(), SyncTickArgs{}.InsertOpts()
	if syncOpts.Queue != Queue || !syncOpts.UniqueOpts.ByArgs || syncOpts.MaxAttempts != syncMaxAttempts ||
		tickOpts.Queue != Queue || !tickOpts.UniqueOpts.ByArgs {
		t.Fatalf("insert options %+v and %+v", syncOpts, tickOpts)
	}
	if (GitHubSyncArgs{}).Kind() != "github_sync" || (SyncTickArgs{}).Kind() != "github_sync_tick" {
		t.Fatal("job kinds changed")
	}
}

func TestTickArgsCarryTheUTCMinute(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 10, 7, 2, 30, 59, 0, time.UTC) }

	args, opts := tickArgs(clock)()

	if got := args.(SyncTickArgs).Minute; got != "2026-10-07T02:30" || opts != nil {
		t.Fatalf("minute %q, options %v", got, opts)
	}
}
