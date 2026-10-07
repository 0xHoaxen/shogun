package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	shinobiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/shinobi/v1"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
)

var seedTime = time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)

// posting stores a posting of owner in a new source, scored when score >= 0.
func (h *harness) posting(t *testing.T, owner, title string, score float32) string {
	t.Helper()
	repo := store.New(h.pool)
	ownerID := uuid.MustParse(owner)
	src := store.NewID()
	input := domain.SourceInput{Name: title, Kind: domain.KindRSS, Schedule: domain.DefaultSchedule, Config: domain.SourceConfig{URL: feedURL}}
	if _, err := repo.InsertSource(context.Background(), store.NewSource{ID: src, OwnerID: ownerID, Input: input}); err != nil {
		t.Fatalf("source: %v", err)
	}
	posted := seedTime
	row, err := repo.UpsertPosting(context.Background(), store.NewPosting{
		ID: store.NewID(), OwnerID: ownerID, SourceID: src, CreatedAt: seedTime, Raw: []byte(`{}`),
		Candidate: domain.Candidate{ExternalID: "1", Title: title, Company: "Acme", URL: "https://acme.example/1", Location: "Remote", PostedAt: &posted},
	})
	if err != nil {
		t.Fatalf("posting: %v", err)
	}
	if score >= 0 {
		if err := repo.SaveScore(context.Background(), row.ID, domain.Score{Value: score, Reasons: []string{"because"}}, "rule", seedTime); err != nil {
			t.Fatalf("score: %v", err)
		}
	}
	return row.ID.String()
}

func TestGetPreferencesDefaultsThenSetPreferencesRoundTrips(t *testing.T) {
	h := newHarness(t)

	before, beforeErr := h.client.GetPreferences(h.ctx(t), &shinobiv1.GetPreferencesRequest{})
	set, setErr := h.client.SetPreferences(h.ctx(t), &shinobiv1.SetPreferencesRequest{Preferences: &shinobiv1.Preferences{
		Roles: []string{" Backend Engineer "}, Locations: []string{"Remote"}, MustHave: []string{"Go"}, Exclude: []string{"unpaid"}, MinScore: 0.6,
	}})
	after, _ := h.client.GetPreferences(h.ctx(t), &shinobiv1.GetPreferencesRequest{})
	other, _ := h.client.GetPreferences(h.ctxFor(t, uuid.NewString()), &shinobiv1.GetPreferencesRequest{})

	if beforeErr != nil || len(before.GetPreferences().GetRoles()) != 0 || before.GetPreferences().GetMinScore() != 0.7 {
		t.Fatalf("before = %v, %v", before, beforeErr)
	}
	got := after.GetPreferences()
	if setErr != nil || got.GetRoles()[0] != "backend engineer" || got.GetLocations()[0] != "remote" || got.GetMustHave()[0] != "go" ||
		got.GetExclude()[0] != "unpaid" || got.GetMinScore() != 0.6 || set.GetPreferences().GetMinScore() != 0.6 {
		t.Fatalf("after = %v, set = %v, %v", after, set, setErr)
	}
	if len(other.GetPreferences().GetRoles()) != 0 {
		t.Fatalf("another owner sees %v", other)
	}
}

func TestSetPreferencesRefusesAMinimumOutsideZeroToOne(t *testing.T) {
	h := newHarness(t)

	_, high := h.client.SetPreferences(h.ctx(t), &shinobiv1.SetPreferencesRequest{Preferences: &shinobiv1.Preferences{MinScore: 1.5}})
	_, low := h.client.SetPreferences(h.ctx(t), &shinobiv1.SetPreferencesRequest{Preferences: &shinobiv1.Preferences{MinScore: -0.1}})

	requireStatus(t, high, codes.InvalidArgument, "INVALID_ARGUMENT")
	requireStatus(t, low, codes.InvalidArgument, "INVALID_ARGUMENT")
}

func TestListPostingsReturnsBestScoreFirstWithUnscoredLastAndTheirReasons(t *testing.T) {
	h := newHarness(t)
	low := h.posting(t, h.owner, "low", 0.3)
	unscored := h.posting(t, h.owner, "unscored", -1)
	high := h.posting(t, h.owner, "high", 0.9)
	h.posting(t, uuid.NewString(), "someone else's", 0.99)

	res, err := h.client.ListPostings(h.ctx(t), &shinobiv1.ListPostingsRequest{})

	if err != nil || len(res.GetPostings()) != 3 {
		t.Fatalf("res %v, err %v", res, err)
	}
	ids := []string{res.GetPostings()[0].GetId(), res.GetPostings()[1].GetId(), res.GetPostings()[2].GetId()}
	if ids[0] != high || ids[1] != low || ids[2] != unscored {
		t.Fatalf("order = %v, want high, low, unscored", ids)
	}
	first, last := res.GetPostings()[0], res.GetPostings()[2]
	if !first.GetScored() || first.GetScore() != 0.9 || first.GetScoredBy() != "rule" || len(first.GetReasons()) != 1 ||
		first.GetTitle() != "high" || first.GetCompany() != "Acme" || first.GetUrl() != "https://acme.example/1" ||
		first.GetLocation() != "Remote" || first.GetPostedAt() == nil || first.GetSavedJobId() != "" {
		t.Fatalf("first = %+v", first)
	}
	if last.GetScored() || last.GetScore() != 0 || len(last.GetReasons()) != 0 {
		t.Fatalf("unscored = %+v", last)
	}
}

func TestListPostingsFiltersByMinimumScoreAndPages(t *testing.T) {
	h := newHarness(t)
	for _, s := range []float32{0.9, 0.8, 0.7, 0.2} {
		h.posting(t, h.owner, "p", s)
	}

	good, goodErr := h.client.ListPostings(h.ctx(t), &shinobiv1.ListPostingsRequest{MinScore: 0.75})
	first, _ := h.client.ListPostings(h.ctx(t), &shinobiv1.ListPostingsRequest{PageSize: 3})
	second, _ := h.client.ListPostings(h.ctx(t), &shinobiv1.ListPostingsRequest{PageSize: 3, PageToken: first.GetNextPageToken()})
	_, tokenErr := h.client.ListPostings(h.ctx(t), &shinobiv1.ListPostingsRequest{PageToken: "!!"})
	_, idErr := h.client.ListPostings(h.ctx(t), &shinobiv1.ListPostingsRequest{SourceId: "nope"})

	if goodErr != nil || len(good.GetPostings()) != 2 {
		t.Fatalf("good = %v, %v; want the two at or above 0.75", good, goodErr)
	}
	if len(first.GetPostings()) != 3 || len(second.GetPostings()) != 1 || second.GetNextPageToken() != "" {
		t.Fatalf("pages %d then %d", len(first.GetPostings()), len(second.GetPostings()))
	}
	requireStatus(t, tokenErr, codes.InvalidArgument, "INVALID_PAGE_TOKEN")
	requireStatus(t, idErr, codes.InvalidArgument, "INVALID_ID")
}

func TestSaveToTrackerReturnsTheJobAndTheListShowsIt(t *testing.T) {
	h := newHarness(t)
	id := h.posting(t, h.owner, "Backend Engineer", 0.9)

	first, err := h.client.SaveToTracker(h.ctx(t), &shinobiv1.SaveToTrackerRequest{PostingId: id})
	again, _ := h.client.SaveToTracker(h.ctx(t), &shinobiv1.SaveToTrackerRequest{PostingId: id})
	listed, _ := h.client.ListPostings(h.ctx(t), &shinobiv1.ListPostingsRequest{})

	if err != nil || first.GetJobId() != h.tracker.jobID || again.GetJobId() != first.GetJobId() || len(h.tracker.keys) != 1 || h.tracker.keys[0] != id {
		t.Fatalf("first %v (%v), again %v, kagami keys %v; want one call keyed by the posting", first, err, again, h.tracker.keys)
	}
	if listed.GetPostings()[0].GetSavedJobId() != first.GetJobId() {
		t.Fatalf("listed saved job = %q, want %q", listed.GetPostings()[0].GetSavedJobId(), first.GetJobId())
	}
}

func TestSaveToTrackerFailuresHaveStableReasons(t *testing.T) {
	h := newHarness(t)
	id := h.posting(t, h.owner, "Backend Engineer", 0.9)
	foreign := h.posting(t, uuid.NewString(), "theirs", 0.9)

	_, missing := h.client.SaveToTracker(h.ctx(t), &shinobiv1.SaveToTrackerRequest{PostingId: uuid.NewString()})
	_, notMine := h.client.SaveToTracker(h.ctx(t), &shinobiv1.SaveToTrackerRequest{PostingId: foreign})
	_, bad := h.client.SaveToTracker(h.ctx(t), &shinobiv1.SaveToTrackerRequest{PostingId: "nope"})
	h.tracker.err = status.Error(codes.InvalidArgument, "no")
	_, refused := h.client.SaveToTracker(h.ctx(t), &shinobiv1.SaveToTrackerRequest{PostingId: id})
	h.tracker.err = status.Error(codes.Unavailable, "down")
	_, down := h.client.SaveToTracker(h.ctx(t), &shinobiv1.SaveToTrackerRequest{PostingId: id})

	requireStatus(t, missing, codes.NotFound, "POSTING_NOT_FOUND")
	requireStatus(t, notMine, codes.NotFound, "POSTING_NOT_FOUND")
	requireStatus(t, bad, codes.InvalidArgument, "INVALID_ID")
	requireStatus(t, refused, codes.FailedPrecondition, "POSTING_NOT_SAVABLE")
	requireStatus(t, down, codes.Unavailable, "TRACKER_UNAVAILABLE")
}
