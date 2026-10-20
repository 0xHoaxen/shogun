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
)

type fakeRoller struct {
	err   error
	calls int
	owner uuid.UUID
}

func (f *fakeRoller) Rollup(_ context.Context, owner uuid.UUID) error {
	f.calls++
	f.owner = owner
	return f.err
}

type fakeOwners struct {
	list []owners.Owner
	err  error
}

func (f fakeOwners) ActiveOwners(context.Context) ([]owners.Owner, error) { return f.list, f.err }

type queued struct {
	owner uuid.UUID
	date  string
}

type fakeEnqueuer struct {
	mu      sync.Mutex
	got     []queued
	failFor map[uuid.UUID]error
}

func (f *fakeEnqueuer) EnqueueRollup(_ context.Context, owner uuid.UUID, date string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failFor[owner]; err != nil {
		return err
	}
	f.got = append(f.got, queued{owner, date})
	return nil
}

func rollup(roller Roller, ownerID string) error {
	w := &rollupWorker{roller: roller, log: slog.New(slog.DiscardHandler)}
	return w.Work(context.Background(), &river.Job[RollupArgs]{
		JobRow: &rivertype.JobRow{ID: 1}, Args: RollupArgs{OwnerID: ownerID, Date: "2026-10-08"},
	})
}

func tick(list []owners.Owner, enq Enqueuer, now time.Time) error {
	w := &tickWorker{owners: fakeOwners{list: list}, enqueuer: enq, now: func() time.Time { return now }}
	return w.Work(context.Background(), &river.Job[RollupTickArgs]{})
}

func TestTheWorkerRollsUpTheJobsOwnerAndReturnsAFailureForRetry(t *testing.T) {
	boom := errors.New("db down")
	for name, tt := range map[string]struct {
		roller  *fakeRoller
		wantErr bool
	}{"ok": {&fakeRoller{}, false}, "failure": {&fakeRoller{err: boom}, true}} {
		t.Run(name, func(t *testing.T) {
			owner := uuid.New()

			err := rollup(tt.roller, owner.String())

			if (err != nil) != tt.wantErr || tt.roller.calls != 1 || tt.roller.owner != owner || (tt.wantErr && !errors.Is(err, boom)) {
				t.Fatalf("err %v, calls %d, owner %s", err, tt.roller.calls, tt.roller.owner)
			}
		})
	}
}

func TestTheWorkerCancelsAnOwnerThatIsNotAUUID(t *testing.T) {
	roller := &fakeRoller{}

	err := rollup(roller, "nope")

	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) || roller.calls != 0 {
		t.Fatalf("err %v, calls %d; want the job cancelled before any work", err, roller.calls)
	}
}

func TestTickQueuesOneRollupPerOwnerWhoseClockHasReachedOneInTheNight(t *testing.T) {
	// 20:00 UTC on the 7th: 01:30 on the 8th in Kolkata and 05:00 on the 8th in Tokyo, both past 01:00.
	now := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	kolkata := owners.Owner{ID: uuid.New(), Timezone: "Asia/Kolkata"}
	tokyo := owners.Owner{ID: uuid.New(), Timezone: "Asia/Tokyo"}
	enq := &fakeEnqueuer{}

	err := tick([]owners.Owner{kolkata, tokyo}, enq, now)

	want := []queued{{kolkata.ID, "2026-10-08"}, {tokyo.ID, "2026-10-08"}}
	if err != nil || len(enq.got) != 2 || enq.got[0] != want[0] || enq.got[1] != want[1] {
		t.Fatalf("err %v, queued %+v, want %+v", err, enq.got, want)
	}
}

func TestTickSkipsAnOwnerWhoseClockHasNotReachedOne(t *testing.T) {
	// 19:00 UTC on the 7th is 00:30 on the 8th in Kolkata, half an hour before their rollup time.
	now := time.Date(2026, 10, 7, 19, 0, 0, 0, time.UTC)
	kolkata := owners.Owner{ID: uuid.New(), Timezone: "Asia/Kolkata"}
	enq := &fakeEnqueuer{}

	err := tick([]owners.Owner{kolkata}, enq, now)

	if err != nil || len(enq.got) != 0 {
		t.Fatalf("err %v, queued %+v, want none before 01:00 local", err, enq.got)
	}
}

func TestTickCatchesUpAnOwnerWhoseTimeAlreadyPassedToday(t *testing.T) {
	// 18:00 UTC on the 7th is 23:30 on the 7th in Kolkata: past 01:00, so the 7th is queued
	// even if an earlier tick was missed. The job is unique by owner and date, so this is safe.
	now := time.Date(2026, 10, 7, 18, 0, 0, 0, time.UTC)
	kolkata := owners.Owner{ID: uuid.New(), Timezone: "Asia/Kolkata"}
	enq := &fakeEnqueuer{}

	err := tick([]owners.Owner{kolkata}, enq, now)

	if err != nil || len(enq.got) != 1 || enq.got[0] != (queued{kolkata.ID, "2026-10-07"}) {
		t.Fatalf("err %v, queued %+v, want the owner's own date, the 7th", err, enq.got)
	}
}

func TestTickKeepsGoingPastAnOwnerItCannotQueueAndReportsIt(t *testing.T) {
	now := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
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

	err := w.Work(context.Background(), &river.Job[RollupTickArgs]{})

	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the failure", err)
	}
}

func TestSetupTicksEveryFifteenMinutesAndEveryJobIsUniqueOnTheSenseiQueue(t *testing.T) {
	setup := NewSetup(&fakeRoller{}, fakeOwners{}, &fakeEnqueuer{}, time.Now, slog.New(slog.DiscardHandler))

	if len(setup.PeriodicJobs) != 1 || setup.Queues[Queue].MaxWorkers != 1 {
		t.Fatalf("setup = %+v", setup)
	}
	rollupOpts, tickOpts := RollupArgs{}.InsertOpts(), RollupTickArgs{}.InsertOpts()
	if rollupOpts.Queue != Queue || !rollupOpts.UniqueOpts.ByArgs || rollupOpts.MaxAttempts != rollupMaxAttempts ||
		tickOpts.Queue != Queue || !tickOpts.UniqueOpts.ByArgs {
		t.Fatalf("insert options %+v and %+v", rollupOpts, tickOpts)
	}
	if (RollupArgs{}).Kind() != "rollup" || (RollupTickArgs{}).Kind() != "rollup_tick" {
		t.Fatal("job kinds changed")
	}
}

func TestTickArgsCarryTheUTCMinute(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 10, 7, 20, 0, 59, 0, time.UTC) }

	args, opts := tickArgs(clock)()

	if got := args.(RollupTickArgs).Minute; got != "2026-10-07T20:00" || opts != nil {
		t.Fatalf("minute %q, options %v", got, opts)
	}
}
