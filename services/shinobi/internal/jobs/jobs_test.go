package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/0xHoaxen/shogun/pkg/bus/relay"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/jobs"
	"github.com/0xHoaxen/shogun/services/shinobi/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

type fakeScheduler struct {
	due []app.DueRun
	err error
	now time.Time
	loc *time.Location
}

func (f *fakeScheduler) DueSources(_ context.Context, now time.Time, loc *time.Location) ([]app.DueRun, error) {
	f.now, f.loc = now, loc
	return f.due, f.err
}

type fakeEnqueuer struct {
	mu    sync.Mutex
	runs  []app.DueRun
	failA uuid.UUID
}

func (f *fakeEnqueuer) EnqueueRun(_ context.Context, run app.DueRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if run.SourceID == f.failA {
		return errors.New("queue down")
	}
	f.runs = append(f.runs, run)
	return nil
}

type fakeRunner struct {
	res app.RunResult
	err error
	got [2]uuid.UUID
}

func (f *fakeRunner) RunSourceFor(_ context.Context, owner, id uuid.UUID) (app.RunResult, error) {
	f.got = [2]uuid.UUID{owner, id}
	return f.res, f.err
}

var jobNow = time.Date(2026, 10, 7, 4, 0, 0, 0, time.UTC)

func istLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := jobs.Location()
	if err != nil {
		t.Fatalf("location: %v", err)
	}
	return loc
}

func clock() time.Time { return jobNow }

func TestSetupSchedulesTheMinutelyCheckAndSizesItsQueue(t *testing.T) {
	loc, _ := jobs.Location()

	got := jobs.NewSetup(&fakeScheduler{}, &fakeRunner{}, &fakeEnqueuer{}, loc, func() time.Time { return jobNow }, slog.New(slog.DiscardHandler))

	if len(got.PeriodicJobs) != 1 || got.Queues[jobs.Queue].MaxWorkers < 1 || loc.String() != "Asia/Kolkata" {
		t.Fatalf("setup = %+v, location %s", got, loc)
	}
}

func TestTheDueCheckQueuesEveryDueSourceAndKeepsGoingAfterAFailure(t *testing.T) {
	bad := uuid.New()
	due := []app.DueRun{{SourceID: uuid.New(), Slot: "a"}, {SourceID: bad, Slot: "b"}, {SourceID: uuid.New(), Slot: "c"}}
	sched, queue := &fakeScheduler{due: due}, &fakeEnqueuer{failA: bad}

	err := jobs.WorkDue(context.Background(), sched, queue, istLocation(t), clock, jobs.DueSourcesArgs{Minute: "2026-10-07T04:00"})

	if err == nil || len(queue.runs) != 2 || queue.runs[0].Slot != "a" || queue.runs[1].Slot != "c" {
		t.Fatalf("err %v, queued %+v; want the failure reported and the other two queued", err, queue.runs)
	}
	if !sched.now.Equal(jobNow) || sched.loc.String() != "Asia/Kolkata" {
		t.Fatalf("scheduler asked at %v in %v", sched.now, sched.loc)
	}
}

func TestTheDueCheckReportsAFailureToFindSources(t *testing.T) {
	err := jobs.WorkDue(context.Background(), &fakeScheduler{err: errors.New("db down")}, &fakeEnqueuer{}, istLocation(t), clock, jobs.DueSourcesArgs{})

	if err == nil {
		t.Fatal("want the failure returned")
	}
}

func TestTheRunWorkerRunsTheSourceForItsOwner(t *testing.T) {
	runner := &fakeRunner{res: app.RunResult{Fetched: 3, Added: 2}}
	owner, source := uuid.New(), uuid.New()

	err := jobs.WorkRun(context.Background(), runner, jobs.RunSourceArgs{OwnerID: owner, SourceID: source, Slot: "x"})

	if err != nil || runner.got != [2]uuid.UUID{owner, source} {
		t.Fatalf("err %v, runner got %v", err, runner.got)
	}
}

func TestTheRunWorkerLeavesASourceFailureWithTheSourceAndCancelsAGoneSource(t *testing.T) {
	args := jobs.RunSourceArgs{OwnerID: uuid.New(), SourceID: uuid.New()}

	failedErr := jobs.WorkRun(context.Background(), &fakeRunner{err: &app.RunError{Code: app.CodeFetchFailed, Err: errors.New("x")}}, args)
	goneErr := jobs.WorkRun(context.Background(), &fakeRunner{err: app.ErrSourceNotFound}, args)
	brokenErr := jobs.WorkRun(context.Background(), &fakeRunner{err: errors.New("db down")}, args)

	var cancel *river.JobCancelError
	if failedErr != nil || !errors.As(goneErr, &cancel) || brokenErr == nil || errors.As(brokenErr, &cancel) {
		t.Fatalf("failed %v, gone %v, broken %v", failedErr, goneErr, brokenErr)
	}
}

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "shinobi")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := relay.Migrate(ctx, pool); err != nil {
		t.Fatalf("relay migrate: %v", err)
	}
	return pool
}

func TestRiverQueueStartsOneRunPerSourceAndSlot(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	queue, err := jobs.NewRiverQueue(pool)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	owner, source := uuid.New(), uuid.New()
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'run_source'`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	for range 3 {
		if err := queue.EnqueueRun(ctx, app.DueRun{OwnerID: owner, SourceID: source, Slot: "2026-10-07T01:30:00Z"}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	afterOneSlot := count()
	if err := queue.EnqueueRun(ctx, app.DueRun{OwnerID: owner, SourceID: source, Slot: "2026-10-08T01:30:00Z"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if afterOneSlot != 1 || count() != 2 {
		t.Fatalf("jobs after one slot offered three times %d, after a second slot %d; want 1 then 2", afterOneSlot, count())
	}
}
