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

	"github.com/0xHoaxen/shogun/pkg/owners"
)

type fakeScanner struct {
	calls   []string
	staleBy error
	dueBy   error
	owner   uuid.UUID
	date    time.Time
}

func (f *fakeScanner) ScanStaleApplications(_ context.Context, owner uuid.UUID, date time.Time) (int, error) {
	f.calls = append(f.calls, "stale")
	f.owner, f.date = owner, date
	return 0, f.staleBy
}

func (f *fakeScanner) ScanDueFollowUps(_ context.Context, owner uuid.UUID, date time.Time) (int, error) {
	f.calls = append(f.calls, "due")
	f.owner, f.date = owner, date
	return 0, f.dueBy
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

func (f *fakeEnqueuer) EnqueueScan(_ context.Context, owner uuid.UUID, date string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failFor[owner]; err != nil {
		return err
	}
	f.got = append(f.got, queued{owner, date})
	return nil
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

func scan(sc Scanner, ownerID, date string) error {
	w := &scanWorker{scanner: sc, log: quiet()}
	return w.Work(context.Background(), &river.Job[DailyScanArgs]{Args: DailyScanArgs{OwnerID: ownerID, Date: date}})
}

func tick(list []owners.Owner, enq Enqueuer, now time.Time) error {
	w := &tickWorker{owners: fakeOwners{list: list}, enqueuer: enq, now: func() time.Time { return now }}
	return w.Work(context.Background(), &river.Job[ScanTickArgs]{})
}

func TestScanPlansStaleApplicationsBeforeEmittingWhatIsDue(t *testing.T) {
	sc := &fakeScanner{}
	owner := uuid.New()

	err := scan(sc, owner.String(), "2026-10-07")

	if err != nil || len(sc.calls) != 2 || sc.calls[0] != "stale" || sc.calls[1] != "due" {
		t.Fatalf("err %v, calls %v, want stale then due", err, sc.calls)
	}
	if sc.owner != owner || sc.date.Format(time.DateOnly) != "2026-10-07" {
		t.Fatalf("scanned %s on %s", sc.owner, sc.date)
	}
}

func TestScanStopsAtAFailingStaleStepSoNothingIsEmittedOutOfOrder(t *testing.T) {
	boom := errors.New("database is down")
	sc := &fakeScanner{staleBy: boom}

	err := scan(sc, uuid.NewString(), "2026-10-07")

	if !errors.Is(err, boom) || len(sc.calls) != 1 {
		t.Fatalf("err %v, calls %v; want the failure and no due step", err, sc.calls)
	}
}

func TestScanReportsAFailingDueStepSoRiverRetries(t *testing.T) {
	boom := errors.New("database is down")

	err := scan(&fakeScanner{dueBy: boom}, uuid.NewString(), "2026-10-07")

	var cancel *river.JobCancelError
	if !errors.Is(err, boom) || errors.As(err, &cancel) {
		t.Fatalf("got %v, want the plain failure", err)
	}
}

func TestScanCancelsAnOwnerOrDateThatCanNeverWork(t *testing.T) {
	tests := map[string][2]string{
		"owner is not a uuid": {"nope", "2026-10-07"},
		"date is not a date":  {uuid.NewString(), "07/10/2026"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			sc := &fakeScanner{}

			err := scan(sc, args[0], args[1])

			var cancel *river.JobCancelError
			if !errors.As(err, &cancel) || len(sc.calls) != 0 {
				t.Fatalf("err %v, calls %v; want the job cancelled before any work", err, sc.calls)
			}
		})
	}
}

func TestTickQueuesOneScanPerOwnerWhoseClockHasReachedTheScanTime(t *testing.T) {
	// 02:30 UTC on the 7th: 08:00 in Kolkata (past 07:55), 11:30 in Tokyo, 22:30 on the 6th in New York.
	now := time.Date(2026, 10, 7, 2, 30, 0, 0, time.UTC)
	kolkata := owners.Owner{ID: uuid.New(), Timezone: "Asia/Kolkata"}
	tokyo := owners.Owner{ID: uuid.New(), Timezone: "Asia/Tokyo"}
	london := owners.Owner{ID: uuid.New(), Timezone: "Europe/London"} // 03:30, too early
	enq := &fakeEnqueuer{}

	err := tick([]owners.Owner{kolkata, tokyo, london}, enq, now)

	want := []queued{{kolkata.ID, "2026-10-07"}, {tokyo.ID, "2026-10-07"}}
	if err != nil || len(enq.got) != 2 || enq.got[0] != want[0] || enq.got[1] != want[1] {
		t.Fatalf("err %v, queued %+v, want %+v", err, enq.got, want)
	}
}

func TestTickKeepsGoingPastAnOwnerItCannotQueueAndReportsIt(t *testing.T) {
	now := time.Date(2026, 10, 7, 4, 0, 0, 0, time.UTC)
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

	err := w.Work(context.Background(), &river.Job[ScanTickArgs]{})

	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the failure", err)
	}
}

func TestSetupTicksEveryFifteenMinutesAndEveryJobIsUniqueOnTheKagamiQueue(t *testing.T) {
	setup := NewSetup(&fakeScanner{}, fakeOwners{}, &fakeEnqueuer{}, time.Now, quiet())

	if len(setup.PeriodicJobs) != 1 || setup.Queues[Queue].MaxWorkers != 1 || setup.Workers == nil {
		t.Fatalf("got %+v", setup)
	}
	for name, opts := range map[string]river.InsertOpts{
		"scan": DailyScanArgs{}.InsertOpts(), "tick": ScanTickArgs{}.InsertOpts(),
	} {
		if opts.Queue != Queue || !opts.UniqueOpts.ByArgs {
			t.Fatalf("%s job must be unique by its args on the kagami queue: %+v", name, opts)
		}
	}
	if (DailyScanArgs{}).Kind() != "daily_scan" || (ScanTickArgs{}).Kind() != "daily_scan_tick" {
		t.Fatal("job kinds changed")
	}
}

func TestTickArgsCarryTheUTCMinute(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 10, 7, 2, 30, 59, 0, time.UTC) }

	args, opts := tickArgs(clock)()

	if got := args.(ScanTickArgs).Minute; got != "2026-10-07T02:30" || opts != nil {
		t.Fatalf("minute %q, options %v", got, opts)
	}
}
