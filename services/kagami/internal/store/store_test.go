package store_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
	"github.com/0xHoaxen/shogun/services/kagami/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

// newRepo returns a Repo on a freshly migrated kagami schema.
func newRepo(t *testing.T) (context.Context, *store.Repo) {
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
	return ctx, store.New(pool)
}

func ptr[T any](v T) *T { return &v }

func newJob(ctx context.Context, t *testing.T, r *store.Repo, owner uuid.UUID, title string) db.Job {
	t.Helper()
	company, err := r.FindOrCreateCompanyByName(ctx, owner, "Northwind")
	if err != nil {
		t.Fatalf("company: %v", err)
	}
	j, err := r.InsertJob(ctx, db.InsertJobParams{
		ID: store.NewID(), OwnerID: owner, CompanyID: company.ID, Title: title,
		Source: "manual", Status: "saved",
	})
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	return j
}

func newContact(ctx context.Context, r *store.Repo, owner uuid.UUID, name string, email *string) (db.Contact, error) {
	return r.InsertContact(ctx, db.InsertContactParams{
		ID: store.NewID(), OwnerID: owner, FullName: name, Email: email,
		Status: "not_reached", Tags: []string{},
	})
}

func TestJobUpdateChecksVersion(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()
	job := newJob(ctx, t, r, owner, "Backend Engineer")

	edit := db.UpdateJobParams{
		ID: job.ID, OwnerID: owner, Version: job.Version,
		Title: "Senior Backend Engineer", Source: job.Source,
	}
	updated, err := r.UpdateJob(ctx, edit)
	if err != nil {
		t.Fatalf("update with current version: %v", err)
	}
	if updated.Version != job.Version+1 || updated.Title != edit.Title {
		t.Fatalf("got version %d title %q", updated.Version, updated.Title)
	}

	// The same call again carries the version the caller last read, now stale.
	if _, err := r.UpdateJob(ctx, edit); !errors.Is(err, store.ErrVersionConflict) {
		t.Fatalf("stale update: got %v, want ErrVersionConflict", err)
	}
	edit.ID = store.NewID()
	if _, err := r.UpdateJob(ctx, edit); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing job: got %v, want ErrNotFound", err)
	}
	if _, err := r.GetJob(ctx, store.NewID(), job.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other owner: got %v, want ErrNotFound", err)
	}
}

func TestJobStatusUpdateChecksVersion(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()
	job := newJob(ctx, t, r, owner, "SRE")

	arg := db.UpdateJobStatusParams{ID: job.ID, OwnerID: owner, Version: job.Version, Status: "applied"}
	if _, err := r.UpdateJobStatus(ctx, arg); err != nil {
		t.Fatalf("update status: %v", err)
	}
	if _, err := r.UpdateJobStatus(ctx, arg); !errors.Is(err, store.ErrVersionConflict) {
		t.Fatalf("stale status update: got %v, want ErrVersionConflict", err)
	}
}

func TestListJobsPaginatesWithoutGapsOrRepeats(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()
	const total = 5
	want := make([]uuid.UUID, 0, total)
	for range total {
		want = append(want, newJob(ctx, t, r, owner, "Role").ID)
	}
	newJob(ctx, t, r, store.NewID(), "Someone else's role")

	var got []uuid.UUID
	token := ""
	pages := 0
	for {
		rows, next, err := r.ListJobs(ctx, owner, store.JobFilter{}, store.Page{Size: 2, Token: token})
		if err != nil {
			t.Fatalf("list page %d: %v", pages, err)
		}
		for _, j := range rows {
			got = append(got, j.ID)
		}
		pages++
		if next == "" {
			break
		}
		token = next
	}

	if pages != 3 {
		t.Fatalf("got %d pages, want 3", pages)
	}
	if len(got) != total {
		t.Fatalf("got %d jobs, want %d", len(got), total)
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range got {
		if seen[id] {
			t.Fatalf("job %s listed twice", id)
		}
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			t.Fatalf("job %s missing from the listing", id)
		}
	}
}

func TestListJobsFilters(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()
	saved := newJob(ctx, t, r, owner, "Saved role")
	applied := newJob(ctx, t, r, owner, "Applied role")
	if _, err := r.UpdateJobStatus(ctx, db.UpdateJobStatusParams{
		ID: applied.ID, OwnerID: owner, Version: applied.Version, Status: "applied",
	}); err != nil {
		t.Fatalf("update status: %v", err)
	}

	tests := []struct {
		name   string
		filter store.JobFilter
		want   uuid.UUID
	}{
		{"by status saved", store.JobFilter{Status: ptr("saved")}, saved.ID},
		{"by status applied", store.JobFilter{Status: ptr("applied")}, applied.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, _, err := r.ListJobs(ctx, owner, tt.filter, store.Page{})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(rows) != 1 || rows[0].ID != tt.want {
				t.Fatalf("got %d rows, want only %s", len(rows), tt.want)
			}
		})
	}
}

func TestListJobsRejectsBadPageToken(t *testing.T) {
	ctx, r := newRepo(t)
	_, _, err := r.ListJobs(ctx, store.NewID(), store.JobFilter{}, store.Page{Token: "not-a-token"})
	if !errors.Is(err, store.ErrInvalidPageToken) {
		t.Fatalf("got %v, want ErrInvalidPageToken", err)
	}
}

func TestInsertJobRejectsRepeatedURLAndIdempotencyKey(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()
	company, err := r.FindOrCreateCompanyByName(ctx, owner, "Lumen")
	if err != nil {
		t.Fatalf("company: %v", err)
	}
	insert := func(url, key *string) error {
		_, err := r.InsertJob(ctx, db.InsertJobParams{
			ID: store.NewID(), OwnerID: owner, CompanyID: company.ID, Title: "Go Developer",
			Source: "manual", Status: "saved", Url: url, IdempotencyKey: key,
		})
		return err
	}
	if err := insert(ptr("https://lumen.example/jobs/1"), ptr("key-1")); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := insert(ptr("https://lumen.example/jobs/1"), nil); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("repeated url: got %v, want ErrDuplicate", err)
	}
	if err := insert(nil, ptr("key-1")); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("repeated key: got %v, want ErrDuplicate", err)
	}
	got, err := r.GetJobByIdempotencyKey(ctx, owner, "key-1")
	if err != nil || got.Url == nil || *got.Url != "https://lumen.example/jobs/1" {
		t.Fatalf("lookup by key: %+v, %v", got, err)
	}
}

func TestCompanyUpsertByDomainReturnsExistingRow(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()

	first, err := r.UpsertCompanyByDomain(ctx, owner, "Halden Labs", "halden.example")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	again, err := r.UpsertCompanyByDomain(ctx, owner, "Halden Labs Inc", "HALDEN.example")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if again.ID != first.ID || again.Name != "Halden Labs" {
		t.Fatalf("got %s %q, want the first row %s", again.ID, again.Name, first.ID)
	}
}

func TestFindDuplicateMatchesEmailThenLinkedin(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()
	byEmail, err := newContact(ctx, r, owner, "Priya Raman", ptr("Priya@Lumen.example"))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	byLinkedin, err := r.InsertContact(ctx, db.InsertContactParams{
		ID: store.NewID(), OwnerID: owner, FullName: "Arjun Mehta", Status: "not_reached",
		LinkedinUrl: ptr("https://linkedin.example/in/arjun"), Tags: []string{},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	tests := []struct {
		name     string
		email    *string
		linkedin *string
		want     uuid.UUID
		wantErr  error
	}{
		{"email ignoring case", ptr("priya@lumen.example"), nil, byEmail.ID, nil},
		{"email wins over linkedin", ptr("priya@lumen.example"), ptr("https://linkedin.example/in/arjun"), byEmail.ID, nil},
		{"linkedin when email is unknown", ptr("nobody@x.example"), ptr("https://linkedin.example/in/arjun"), byLinkedin.ID, nil},
		{"linkedin only", nil, ptr("https://linkedin.example/in/arjun"), byLinkedin.ID, nil},
		{"nobody matches", ptr("nobody@x.example"), ptr("https://linkedin.example/in/nobody"), uuid.Nil, store.ErrNotFound},
		{"no identifiers", nil, ptr(""), uuid.Nil, store.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.FindDuplicate(ctx, owner, tt.email, tt.linkedin)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}
			if err == nil && got.ID != tt.want {
				t.Fatalf("got %s, want %s", got.ID, tt.want)
			}
		})
	}

	if _, err := newContact(ctx, r, owner, "Priya Again", ptr("PRIYA@lumen.example")); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("insert with a case-different email: got %v, want ErrDuplicate", err)
	}
}

func TestContactUpdateChecksVersionAndListFilters(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()
	c, err := newContact(ctx, r, owner, "Mira Okafor", ptr("mira@corvid.example"))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	arg := db.UpdateContactStatusParams{
		ID: c.ID, OwnerID: owner, Version: c.Version, Status: "reached_out",
		LastContacted: ptr(time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)),
	}
	if _, err := r.UpdateContactStatus(ctx, arg); err != nil {
		t.Fatalf("update status: %v", err)
	}
	if _, err := r.UpdateContactStatus(ctx, arg); !errors.Is(err, store.ErrVersionConflict) {
		t.Fatalf("stale update: got %v, want ErrVersionConflict", err)
	}

	rows, _, err := r.ListContacts(ctx, owner, store.ContactFilter{Status: ptr("reached_out"), Query: ptr("MIRA")}, store.Page{})
	if err != nil || len(rows) != 1 || rows[0].ID != c.ID {
		t.Fatalf("filtered list: %d rows, %v", len(rows), err)
	}
	rows, _, err = r.ListContacts(ctx, owner, store.ContactFilter{Status: ptr("replied")}, store.Page{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("other status: %d rows, %v", len(rows), err)
	}
}
