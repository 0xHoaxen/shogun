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
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
)

type fakeDigester struct {
	wait  time.Duration
	err   error
	owner uuid.UUID
	date  string
	runs  int
}

func (f *fakeDigester) RunFor(_ context.Context, owner uuid.UUID, date string) (time.Duration, error) {
	f.runs++
	f.owner, f.date = owner, date
	return f.wait, f.err
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

func (f *fakeEnqueuer) EnqueueDigest(_ context.Context, owner uuid.UUID, date string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failFor[owner]; err != nil {
		return err
	}
	f.got = append(f.got, queued{owner, date})
	return nil
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

func work(t *testing.T, d Digester, ownerID, date string) error {
	t.Helper()
	w := &digestWorker{digester: d, log: quiet()}
	return w.Work(context.Background(), &river.Job[DailyDigestArgs]{Args: DailyDigestArgs{OwnerID: ownerID, Date: date}})
}

func tick(t *testing.T, list []owners.Owner, enq Enqueuer, now time.Time) error {
	t.Helper()
	w := &tickWorker{owners: fakeOwners{list: list}, enqueuer: enq, now: func() time.Time { return now }, log: quiet()}
	return w.Work(context.Background(), &river.Job[DigestTickArgs]{})
}

func TestWorkRunsTheDigestForTheOwnerAndDateOfTheJob(t *testing.T) {
	d := &fakeDigester{}
	owner := uuid.New()

	err := work(t, d, owner.String(), "2026-10-07")

	if err != nil || d.runs != 1 || d.owner != owner || d.date != "2026-10-07" {
		t.Fatalf("err %v, runs %d, owner %s, date %q", err, d.runs, d.owner, d.date)
	}
}

func TestWorkSnoozesForTheQuietHoursItWasToldAbout(t *testing.T) {
	d := &fakeDigester{wait: 45 * time.Minute}

	err := work(t, d, uuid.NewString(), "2026-10-07")

	var snooze *river.JobSnoozeError
	if !errors.As(err, &snooze) || snooze.Duration != 45*time.Minute {
		t.Fatalf("got %v, want a 45m snooze", err)
	}
}

func TestWorkCancelsAnOwnerOrDateThatCanNeverWork(t *testing.T) {
	tests := []struct {
		name        string
		owner, date string
		digesterErr error
	}{
		{"owner is not a uuid", "nope", "2026-10-07", nil},
		{"date is not a date", uuid.NewString(), "07/10/2026", app.ErrBadDigestDate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := work(t, &fakeDigester{err: tt.digesterErr}, tt.owner, tt.date)

			var cancel *river.JobCancelError
			if !errors.As(err, &cancel) {
				t.Fatalf("got %v, want the job cancelled", err)
			}
		})
	}
}

func TestWorkReturnsAFailureSoRiverRetries(t *testing.T) {
	boom := errors.New("kagami is down")

	err := work(t, &fakeDigester{err: boom}, uuid.NewString(), "2026-10-07")

	var cancel *river.JobCancelError
	var snooze *river.JobSnoozeError
	if !errors.Is(err, boom) || errors.As(err, &cancel) || errors.As(err, &snooze) {
		t.Fatalf("got %v, want the plain failure so River retries", err)
	}
}

func TestTickQueuesOneDigestPerOwnerWhoseClockHasReachedTheDigestTime(t *testing.T) {
	// 03:30 UTC on the 7th: 09:00 in Kolkata (past 08:30), 12:30 in Tokyo, 23:30 on the 6th in New York.
	now := time.Date(2026, 10, 7, 3, 30, 0, 0, time.UTC)
	kolkata := owners.Owner{ID: uuid.New(), Timezone: "Asia/Kolkata"}
	tokyo := owners.Owner{ID: uuid.New(), Timezone: "Asia/Tokyo"}
	london := owners.Owner{ID: uuid.New(), Timezone: "Europe/London"} // 04:30, too early
	enq := &fakeEnqueuer{}

	err := tick(t, []owners.Owner{kolkata, tokyo, london}, enq, now)

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

	err := tick(t, []owners.Owner{broken, fine}, enq, now)

	if !errors.Is(err, boom) || len(enq.got) != 1 || enq.got[0].owner != fine.ID {
		t.Fatalf("err %v, queued %+v; want the failure reported and the other owner queued", err, enq.got)
	}
}

func TestTickReportsAnOwnerListThatCannotBeRead(t *testing.T) {
	boom := errors.New("database is down")
	w := &tickWorker{owners: fakeOwners{err: boom}, enqueuer: &fakeEnqueuer{}, now: time.Now, log: quiet()}

	err := w.Work(context.Background(), &river.Job[DigestTickArgs]{})

	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the failure", err)
	}
}

func TestSetupTicksEveryFifteenMinutesOnItsOwnQueue(t *testing.T) {
	setup := NewSetup(&fakeDigester{}, fakeOwners{}, &fakeEnqueuer{}, time.Now, quiet())

	if len(setup.PeriodicJobs) != 1 {
		t.Fatalf("got %d periodic jobs, want 1", len(setup.PeriodicJobs))
	}
	if cfg, ok := setup.Queues[Queue]; !ok || cfg.MaxWorkers != 1 {
		t.Fatalf("queues %+v, want %q with one worker", setup.Queues, Queue)
	}
	digest := DailyDigestArgs{OwnerID: "o", Date: "2026-10-07"}.InsertOpts()
	tickOpts := DigestTickArgs{}.InsertOpts()
	if digest.Queue != Queue || !digest.UniqueOpts.ByArgs || tickOpts.Queue != Queue || !tickOpts.UniqueOpts.ByArgs {
		t.Fatalf("insert options %+v and %+v, want the taiko queue and unique by args", digest, tickOpts)
	}
	if (DailyDigestArgs{}).Kind() != "daily_digest" || (DigestTickArgs{}).Kind() != "digest_tick" {
		t.Fatal("job kinds changed")
	}
}

func TestTickArgsCarryTheUTCMinute(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 10, 7, 3, 30, 59, 0, time.UTC) }

	args, opts := tickArgs(clock)()

	if got := args.(DigestTickArgs).Minute; got != "2026-10-07T03:30" || opts != nil {
		t.Fatalf("minute %q, options %v", got, opts)
	}
}
