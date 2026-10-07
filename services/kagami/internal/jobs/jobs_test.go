package jobs

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func mustLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(scanLocation)
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return loc
}

func TestScanDateUsesTheLocalCalendar(t *testing.T) {
	loc := mustLocation(t)
	// 20:00 UTC on 3 Oct is 01:30 on 4 Oct in India.
	if got := scanDate(time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC), loc); got != "2026-10-04" {
		t.Fatalf("got %s, want 2026-10-04", got)
	}
}

func TestNewSetupRegistersBothScansOnOneQueue(t *testing.T) {
	setup, err := NewSetup(nil, time.Now, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if len(setup.PeriodicJobs) != 2 || setup.Queues[Queue].MaxWorkers != 1 || setup.Workers == nil {
		t.Fatalf("got %+v", setup)
	}
	opts := FollowUpScanArgs{}.InsertOpts()
	if opts.Queue != Queue || !opts.UniqueOpts.ByArgs {
		t.Fatalf("scan jobs must be unique by their date: %+v", opts)
	}
}

// scanEnv is a migrated schema with a service on a clock the test moves.
type scanEnv struct {
	pool *pgxpool.Pool
	svc  *app.Service
	now  time.Time
}

func newScanEnv(t *testing.T) *scanEnv {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "kagami")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	env := &scanEnv{pool: pool, now: time.Date(2026, 10, 3, 3, 0, 0, 0, time.UTC)}
	env.svc = app.NewService(pool, func() time.Time { return env.now })
	return env
}

func (e *scanEnv) as(owner uuid.UUID) context.Context {
	return authz.WithIdentity(context.Background(), authz.Identity{OwnerID: owner.String()})
}

func (e *scanEnv) addJob(t *testing.T, owner uuid.UUID, title string, status domain.JobStatus) string {
	t.Helper()
	res, err := e.svc.AddJob(e.as(owner), app.AddJobInput{Title: title, CompanyName: "Northwind", Status: status})
	if err != nil {
		t.Fatalf("add job %s: %v", title, err)
	}
	return res.Job.ID.String()
}

func (e *scanEnv) setJobFollowUp(t *testing.T, owner uuid.UUID, id, day string) {
	t.Helper()
	detail, err := e.svc.GetJob(e.as(owner), id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	_, err = e.svc.UpdateJob(e.as(owner), app.UpdateJobInput{
		ID: id, Version: detail.Job.Version, Paths: []string{"next_follow_up"}, Fields: app.JobFields{NextFollowUp: day},
	})
	if err != nil {
		t.Fatalf("set follow-up: %v", err)
	}
}

func (e *scanEnv) outboxCount(t *testing.T, eventType string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE type = $1`, eventType).Scan(&n); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return n
}

func TestDueScanEmitsOneEventPerDueItemAcrossOwners(t *testing.T) {
	env := newScanEnv(t)
	owner, other := uuid.New(), uuid.New()
	due := env.addJob(t, owner, "Due today", domain.JobSaved)
	later := env.addJob(t, owner, "Due tomorrow", domain.JobSaved)
	rejected := env.addJob(t, owner, "Rejected", domain.JobSaved)
	otherDue := env.addJob(t, other, "Other owner's", domain.JobSaved)
	env.setJobFollowUp(t, owner, due, "2026-10-03")
	env.setJobFollowUp(t, owner, later, "2026-10-04")
	env.setJobFollowUp(t, other, otherDue, "2026-10-03")
	if _, err := env.svc.ChangeJobStatus(env.as(owner), app.ChangeJobStatusInput{ID: rejected, To: domain.JobRejected, Version: 1}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	env.setJobFollowUp(t, owner, rejected, "2026-10-03")
	contact, err := env.svc.AddContact(env.as(owner), app.AddContactInput{
		ContactFields: app.ContactFields{FullName: "Arjun Mehta", NextFollowUp: "2026-10-03"},
	})
	if err != nil {
		t.Fatalf("add contact: %v", err)
	}

	emitted, err := env.svc.ScanDueFollowUps(context.Background(), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if emitted != 3 {
		t.Fatalf("emitted %d, want 3: two jobs and one contact", emitted)
	}
	if n := env.outboxCount(t, "job.follow_up_due"); n != 2 {
		t.Fatalf("got %d job.follow_up_due rows, want 2", n)
	}
	if n := env.outboxCount(t, "contact.follow_up_due"); n != 1 {
		t.Fatalf("got %d contact.follow_up_due rows for %s, want 1", n, contact.ID)
	}

	tomorrow, err := env.svc.ScanDueFollowUps(context.Background(), time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	if err != nil || tomorrow != 1 {
		t.Fatalf("next day emitted %d, %v; want only the item due that day", tomorrow, err)
	}
}

func TestStaleScanPlansAFollowUpThatTheDueScanThenEmits(t *testing.T) {
	env := newScanEnv(t)
	owner := uuid.New()

	env.now = time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	stale := env.addJob(t, owner, "Applied long ago", domain.JobApplied)
	planned := env.addJob(t, owner, "Already planned", domain.JobApplied)
	env.setJobFollowUp(t, owner, planned, "2026-10-10")
	env.now = time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	env.addJob(t, owner, "Applied this week", domain.JobApplied)
	env.addJob(t, owner, "Only saved", domain.JobSaved)
	env.now = time.Date(2026, 10, 3, 3, 0, 0, 0, time.UTC)
	date := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)

	count, err := env.svc.ScanStaleApplications(context.Background(), date)
	if err != nil || count != 1 {
		t.Fatalf("planned %d, %v; want 1", count, err)
	}
	detail, err := env.svc.GetJob(env.as(owner), stale)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := detail.Job.NextFollowUp; got == nil || got.Format(time.DateOnly) != "2026-10-03" {
		t.Fatalf("got follow-up %v, want 2026-10-03", got)
	}
	if kind := detail.Events[0].Kind; kind != "follow_up_set" {
		t.Fatalf("newest timeline entry is %q, want follow_up_set", kind)
	}
	if n := env.outboxCount(t, "job.follow_up_due"); n != 0 {
		t.Fatalf("the stale scan wrote %d job.follow_up_due rows, want none", n)
	}

	again, err := env.svc.ScanStaleApplications(context.Background(), date)
	if err != nil || again != 0 {
		t.Fatalf("second stale scan planned %d, %v; want 0", again, err)
	}

	emitted, err := env.svc.ScanDueFollowUps(context.Background(), date)
	if err != nil || emitted != 1 {
		t.Fatalf("due scan emitted %d, %v; want 1", emitted, err)
	}
	if n := env.outboxCount(t, "job.follow_up_due"); n != 1 {
		t.Fatalf("got %d job.follow_up_due rows, want 1", n)
	}
}

func TestScanPayloadNamesTheItemAndTheDueDate(t *testing.T) {
	env := newScanEnv(t)
	owner := uuid.New()
	id := env.addJob(t, owner, "Due today", domain.JobSaved)
	env.setJobFollowUp(t, owner, id, "2026-10-03")

	if _, err := env.svc.ScanDueFollowUps(context.Background(), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("scan: %v", err)
	}

	var subject string
	err := env.pool.QueryRow(context.Background(),
		`SELECT subject FROM outbox WHERE type = 'job.follow_up_due'`).Scan(&subject)
	if err != nil || subject != id {
		t.Fatalf("got subject %q, %v; want the job id %s", subject, err, id)
	}
}
