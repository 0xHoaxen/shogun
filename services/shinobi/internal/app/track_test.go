package app_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
)

// fakeTracker records the jobs it is asked to add and answers with jobID, or
// err. Like kagami, it answers a repeated idempotency key with the first job.
type fakeTracker struct {
	reqs    []*kagamiv1.AddJobRequest
	err     error
	byKey   map[string]string
	noID    bool
	onAdd   func() // runs before the answer, to stage a race
	nextJob func() string
}

func (f *fakeTracker) AddJob(_ context.Context, in *kagamiv1.AddJobRequest, _ ...grpc.CallOption) (*kagamiv1.AddJobResponse, error) {
	f.reqs = append(f.reqs, in)
	if f.err != nil {
		return nil, f.err
	}
	if f.onAdd != nil {
		f.onAdd()
	}
	if f.byKey == nil {
		f.byKey = map[string]string{}
	}
	id, ok := f.byKey[in.GetIdempotencyKey()]
	if !ok {
		id = uuid.NewString()
		if f.nextJob != nil {
			id = f.nextJob()
		}
		f.byKey[in.GetIdempotencyKey()] = id
	}
	if f.noID {
		id = ""
	}
	return &kagamiv1.AddJobResponse{Job: &kagamiv1.Job{Id: id}}, nil
}

func newTrackerService(t *testing.T, tr app.Tracker) (*app.Service, *pgxpool.Pool) {
	t.Helper()
	_, pool := newService(t, &fakeFetcher{})
	return app.NewService(pool, &fakeFetcher{}, func() time.Time { return testNow }, slog.New(slog.DiscardHandler), app.WithTracker(tr)), pool
}

// postWith stores a posting with the given company, link, location and description.
func postWith(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID, company, link, location, description string) uuid.UUID {
	t.Helper()
	repo := store.New(pool)
	src := store.NewID()
	if _, err := repo.InsertSource(context.Background(), store.NewSource{ID: src, OwnerID: owner, Input: rssInput("s")}); err != nil {
		t.Fatalf("source: %v", err)
	}
	row, err := repo.UpsertPosting(context.Background(), store.NewPosting{
		ID: store.NewID(), OwnerID: owner, SourceID: src, CreatedAt: testNow, Raw: []byte(`{"description":"` + description + `","item":{}}`),
		Candidate: domain.Candidate{ExternalID: "1", Title: "Backend Engineer", Company: company, URL: link, Location: location},
	})
	if err != nil {
		t.Fatalf("posting: %v", err)
	}
	return row.ID
}

func TestSaveToTrackerAddsASavedJobAndRemembersIt(t *testing.T) {
	tracker := &fakeTracker{}
	svc, pool := newTrackerService(t, tracker)
	owner := uuid.New()
	id := postWith(t, pool, owner, "Acme", "https://acme.example/jobs/1", "Remote", "We use Go.")

	job, err := svc.SaveToTracker(asOwner(owner), id)

	if err != nil || job == uuid.Nil || len(tracker.reqs) != 1 {
		t.Fatalf("job %s, err %v, requests %d", job, err, len(tracker.reqs))
	}
	req := tracker.reqs[0]
	if req.GetTitle() != "Backend Engineer" || req.GetCompanyName() != "Acme" || req.GetUrl() != "https://acme.example/jobs/1" ||
		req.GetSource() != "discovery" || req.GetStatus() != kagamiv1.JobStatus_JOB_STATUS_SAVED || req.GetLocation() != "Remote" ||
		req.GetDescription() != "We use Go." || req.GetIdempotencyKey() != id.String() {
		t.Fatalf("request = %+v", req)
	}
	row, _ := store.New(pool).GetPosting(context.Background(), owner, id)
	if row.SavedJobID == nil || *row.SavedJobID != job {
		t.Fatalf("saved job = %v, want %s", row.SavedJobID, job)
	}
}

func TestSavingAgainReturnsTheSameJobWithoutAskingKagamiAgain(t *testing.T) {
	tracker := &fakeTracker{}
	svc, pool := newTrackerService(t, tracker)
	owner := uuid.New()
	id := postWith(t, pool, owner, "Acme", "https://acme.example/1", "", "")

	first, firstErr := svc.SaveToTracker(asOwner(owner), id)
	second, secondErr := svc.SaveToTracker(asOwner(owner), id)

	if firstErr != nil || secondErr != nil || first != second || len(tracker.reqs) != 1 {
		t.Fatalf("first %s %v, second %s %v, requests %d; want one job and one request", first, firstErr, second, secondErr, len(tracker.reqs))
	}
}

func TestSavingAfterAFailureAfterKagamiMadeTheJobGetsTheSameJob(t *testing.T) {
	tracker := &fakeTracker{}
	svc, pool := newTrackerService(t, tracker)
	owner := uuid.New()
	id := postWith(t, pool, owner, "Acme", "https://acme.example/1", "", "")
	// The first call made the job in kagami but never recorded it here.
	madeBefore := uuid.NewString()
	tracker.byKey = map[string]string{id.String(): madeBefore}

	job, err := svc.SaveToTracker(asOwner(owner), id)

	if err != nil || job.String() != madeBefore {
		t.Fatalf("job %s, err %v; want kagami's original %s", job, err, madeBefore)
	}
}

func TestSaveToTrackerNamesTheCompanyFromTheLinkOrAPlaceholder(t *testing.T) {
	tests := []struct {
		name    string
		company string
		link    string
		want    string
	}{
		{"company given", "Acme", "https://acme.example/1", "Acme"},
		{"company from the link's host", "", "https://jobs.example.com/1", "jobs.example.com"},
		{"nothing to go on", "", "", "Unknown company"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := &fakeTracker{}
			svc, pool := newTrackerService(t, tracker)
			owner := uuid.New()
			id := postWith(t, pool, owner, tt.company, tt.link, "", "")

			_, err := svc.SaveToTracker(asOwner(owner), id)

			if err != nil || tracker.reqs[0].GetCompanyName() != tt.want {
				t.Fatalf("err %v, company %q, want %q", err, tracker.reqs[0].GetCompanyName(), tt.want)
			}
		})
	}
}

func TestSaveToTrackerKeepsTheFirstJobWhenAnotherCallWonTheRace(t *testing.T) {
	winner := uuid.New()
	var svc *app.Service
	var pool *pgxpool.Pool
	var owner, posting uuid.UUID
	tracker := &fakeTracker{onAdd: func() {
		if _, _, err := store.New(pool).SetSavedJob(context.Background(), owner, posting, winner); err != nil {
			t.Errorf("stage race: %v", err)
		}
	}}
	svc, pool = newTrackerService(t, tracker)
	owner = uuid.New()
	posting = postWith(t, pool, owner, "Acme", "https://acme.example/1", "", "")

	job, err := svc.SaveToTracker(asOwner(owner), posting)

	if err != nil || job != winner {
		t.Fatalf("job %s, err %v; want the first recorded job %s", job, err, winner)
	}
}

func TestSaveToTrackerRefusesWhatItCannotSave(t *testing.T) {
	tests := []struct {
		name    string
		tracker *fakeTracker
		want    error
	}{
		{"kagami refuses it", &fakeTracker{err: status.Error(codes.InvalidArgument, "title too long")}, app.ErrPostingRefused},
		{"kagami precondition", &fakeTracker{err: status.Error(codes.FailedPrecondition, "x")}, app.ErrPostingRefused},
		{"kagami down", &fakeTracker{err: status.Error(codes.Unavailable, "down")}, app.ErrTrackerUnavailable},
		{"kagami times out", &fakeTracker{err: status.Error(codes.DeadlineExceeded, "slow")}, app.ErrTrackerUnavailable},
		{"kagami fails", &fakeTracker{err: status.Error(codes.Internal, "boom")}, app.ErrTrackerUnavailable},
		{"kagami answers without a job", &fakeTracker{noID: true}, app.ErrTrackerUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, pool := newTrackerService(t, tt.tracker)
			owner := uuid.New()
			id := postWith(t, pool, owner, "Acme", "https://acme.example/1", "", "")

			_, err := svc.SaveToTracker(asOwner(owner), id)

			row, _ := store.New(pool).GetPosting(context.Background(), owner, id)
			if !errors.Is(err, tt.want) || row.SavedJobID != nil {
				t.Fatalf("err %v, saved job %v; want %v and nothing recorded", err, row.SavedJobID, tt.want)
			}
		})
	}
}

func TestSaveToTrackerIsPerOwnerAndNeedsATracker(t *testing.T) {
	tracker := &fakeTracker{}
	svc, pool := newTrackerService(t, tracker)
	owner := uuid.New()
	id := postWith(t, pool, owner, "Acme", "https://acme.example/1", "", "")
	bare, _ := newService(t, &fakeFetcher{})

	_, foreign := svc.SaveToTracker(asOwner(uuid.New()), id)
	_, missing := svc.SaveToTracker(asOwner(owner), uuid.New())
	_, noOwner := svc.SaveToTracker(context.Background(), id)
	_, noTracker := bare.SaveToTracker(asOwner(owner), id)

	if !errors.Is(foreign, app.ErrPostingNotFound) || !errors.Is(missing, app.ErrPostingNotFound) || !errors.Is(noOwner, app.ErrNoOwner) ||
		!errors.Is(noTracker, app.ErrTrackerUnavailable) || len(tracker.reqs) != 0 {
		t.Fatalf("foreign %v, missing %v, no owner %v, no tracker %v, requests %d", foreign, missing, noOwner, noTracker, len(tracker.reqs))
	}
}
