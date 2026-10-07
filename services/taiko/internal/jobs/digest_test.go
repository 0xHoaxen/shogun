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

type fakeDigester struct {
	wait time.Duration
	err  error
	got  time.Time
	runs int
}

func (f *fakeDigester) Run(_ context.Context, date time.Time) (time.Duration, error) {
	f.runs++
	f.got = date
	return f.wait, f.err
}

func location(t *testing.T) *time.Location {
	t.Helper()
	loc, err := Location()
	if err != nil {
		t.Fatalf("location: %v", err)
	}
	return loc
}

func work(t *testing.T, d Digester, date string) error {
	t.Helper()
	w := &digestWorker{digester: d, loc: location(t), log: slog.New(slog.DiscardHandler)}
	return w.Work(context.Background(), &river.Job[DailyDigestArgs]{Args: DailyDigestArgs{Date: date}})
}

func TestDigestFiresAt0830IndianTime(t *testing.T) {
	loc := location(t)
	at := dailySchedule{hour: digestHour, minute: digestMinute, loc: loc}
	tests := []struct {
		name  string
		after time.Time
		want  time.Time
	}{
		{"before the time", time.Date(2026, 10, 7, 8, 29, 0, 0, loc), time.Date(2026, 10, 7, 8, 30, 0, 0, loc)},
		{"at the time", time.Date(2026, 10, 7, 8, 30, 0, 0, loc), time.Date(2026, 10, 8, 8, 30, 0, 0, loc)},
		{"after the time", time.Date(2026, 10, 7, 20, 0, 0, 0, loc), time.Date(2026, 10, 8, 8, 30, 0, 0, loc)},
		{"asked in UTC", time.Date(2026, 10, 7, 2, 0, 0, 0, time.UTC), time.Date(2026, 10, 7, 8, 30, 0, 0, loc)},
		{"across a month end", time.Date(2026, 10, 31, 9, 0, 0, 0, loc), time.Date(2026, 11, 1, 8, 30, 0, 0, loc)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := at.Next(tt.after); !got.Equal(tt.want) {
				t.Fatalf("Next(%s) = %s, want %s", tt.after, got, tt.want)
			}
		})
	}
}

func TestWorkRunsTheDigestForTheDateOfTheJob(t *testing.T) {
	d := &fakeDigester{}

	err := work(t, d, "2026-10-07")

	if err != nil || d.runs != 1 || d.got.Format(time.DateOnly) != "2026-10-07" || d.got.Location().String() != "Asia/Kolkata" {
		t.Fatalf("err %v, runs %d, date %s", err, d.runs, d.got)
	}
}

func TestWorkSnoozesForTheQuietHoursItWasToldAbout(t *testing.T) {
	d := &fakeDigester{wait: 45 * time.Minute}

	err := work(t, d, "2026-10-07")

	var snooze *rivertype.JobSnoozeError
	if !errors.As(err, &snooze) || snooze.Duration != 45*time.Minute {
		t.Fatalf("got %v, want a snooze of 45m", err)
	}
}

func TestWorkCancelsADateThatIsNotADate(t *testing.T) {
	for _, date := range []string{"", "tomorrow", "2026-13-40", "07-10-2026"} {
		t.Run(date, func(t *testing.T) {
			d := &fakeDigester{}

			err := work(t, d, date)

			var cancel *rivertype.JobCancelError
			if !errors.As(err, &cancel) || d.runs != 0 {
				t.Fatalf("got %v with %d runs, want a cancel without running", err, d.runs)
			}
		})
	}
}

func TestWorkReturnsAFailureSoRiverRetries(t *testing.T) {
	boom := errors.New("kagami is down")
	d := &fakeDigester{err: boom}

	err := work(t, d, "2026-10-07")

	var cancel *rivertype.JobCancelError
	var snooze *rivertype.JobSnoozeError
	if !errors.Is(err, boom) || errors.As(err, &cancel) || errors.As(err, &snooze) {
		t.Fatalf("got %v, want the plain failure", err)
	}
}

func TestSetupSchedulesOneDailyJobOnItsOwnQueue(t *testing.T) {
	loc := location(t)
	setup := NewSetup(&fakeDigester{}, loc, time.Now, slog.New(slog.DiscardHandler))

	if len(setup.PeriodicJobs) != 1 {
		t.Fatalf("got %d periodic jobs, want 1", len(setup.PeriodicJobs))
	}
	if cfg, ok := setup.Queues[Queue]; !ok || cfg.MaxWorkers != 1 {
		t.Fatalf("queues %+v, want %q with one worker", setup.Queues, Queue)
	}
	opts := DailyDigestArgs{Date: "2026-10-07"}.InsertOpts()
	if opts.Queue != Queue || !opts.UniqueOpts.ByArgs {
		t.Fatalf("insert options %+v, want the taiko queue and one job per date", opts)
	}
	if got := (DailyDigestArgs{}).Kind(); got != "daily_digest" {
		t.Fatalf("kind %q", got)
	}
}

func TestScheduledRunCarriesTodaysDateInTheOwnersZone(t *testing.T) {
	loc := location(t)
	// 20:00 UTC on the 6th is already the 7th in Kolkata.
	clock := func() time.Time { return time.Date(2026, 10, 6, 20, 0, 0, 0, time.UTC) }

	args, opts := digestArgs(clock, loc)()

	if got := args.(DailyDigestArgs).Date; got != "2026-10-07" || opts != nil {
		t.Fatalf("job date %q, options %v; want 2026-10-07 and the args' own options", got, opts)
	}
}
