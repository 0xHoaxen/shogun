package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
)

var seedTime = time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)

// seed stores an open suggestion for owner created i minutes after seedTime.
func (h *harness) seed(t *testing.T, owner string, target domain.Target, i int) string {
	t.Helper()
	id := store.NewID()
	_, err := store.New(h.pool).InsertSuggestion(context.Background(), store.NewSuggestion{
		ID: id, OwnerID: uuid.MustParse(owner), CreatedAt: seedTime.Add(time.Duration(i) * time.Minute),
		Suggestion: domain.Suggestion{
			Target: target, Section: domain.SectionProjects, After: "Built a worker pool", Reason: "merged PR",
			Evidence: []domain.Evidence{{Label: "PR 2", URL: "https://github.com/x/y/pull/2"}},
		},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return id.String()
}

func TestListSuggestionsReturnsTheOwnersNewestFirstWithEvidence(t *testing.T) {
	h := newHarness(t, true)
	first := h.seed(t, h.owner, domain.TargetResume, 0)
	second := h.seed(t, h.owner, domain.TargetLinkedIn, 1)
	h.seed(t, uuid.NewString(), domain.TargetResume, 2)

	res, err := h.client.ListSuggestions(h.ctx(t), &katanav1.ListSuggestionsRequest{})

	if err != nil || len(res.GetSuggestions()) != 2 || res.GetSuggestions()[0].GetId() != second || res.GetSuggestions()[1].GetId() != first {
		t.Fatalf("res %v, err %v", res, err)
	}
	got := res.GetSuggestions()[0]
	if got.GetTarget() != katanav1.SuggestionTarget_SUGGESTION_TARGET_LINKEDIN || got.GetState() != katanav1.SuggestionState_SUGGESTION_STATE_OPEN ||
		got.GetSection() != "projects" || got.GetAfter() != "Built a worker pool" || len(got.GetEvidence()) != 1 ||
		got.GetEvidence()[0].GetUrl() != "https://github.com/x/y/pull/2" || got.GetDecidedAt() != nil {
		t.Fatalf("suggestion = %+v", got)
	}
}

func TestListSuggestionsFiltersAndPages(t *testing.T) {
	h := newHarness(t, true)
	for i := range 3 {
		h.seed(t, h.owner, domain.TargetResume, i)
	}
	dismissed := h.seed(t, h.owner, domain.TargetLinkedIn, 3)
	if _, err := h.client.DismissSuggestion(h.ctx(t), &katanav1.DismissSuggestionRequest{Id: dismissed}); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	open, openErr := h.client.ListSuggestions(h.ctx(t), &katanav1.ListSuggestionsRequest{State: katanav1.SuggestionState_SUGGESTION_STATE_OPEN})
	gone, goneErr := h.client.ListSuggestions(h.ctx(t), &katanav1.ListSuggestionsRequest{State: katanav1.SuggestionState_SUGGESTION_STATE_DISMISSED})
	linkedin, _ := h.client.ListSuggestions(h.ctx(t), &katanav1.ListSuggestionsRequest{Target: katanav1.SuggestionTarget_SUGGESTION_TARGET_LINKEDIN})
	first, _ := h.client.ListSuggestions(h.ctx(t), &katanav1.ListSuggestionsRequest{PageSize: 3})
	second, _ := h.client.ListSuggestions(h.ctx(t), &katanav1.ListSuggestionsRequest{PageSize: 3, PageToken: first.GetNextPageToken()})
	_, tokenErr := h.client.ListSuggestions(h.ctx(t), &katanav1.ListSuggestionsRequest{PageToken: "!!"})

	if openErr != nil || len(open.GetSuggestions()) != 3 || goneErr != nil || len(gone.GetSuggestions()) != 1 || len(linkedin.GetSuggestions()) != 1 {
		t.Fatalf("open %d, dismissed %d, linkedin %d", len(open.GetSuggestions()), len(gone.GetSuggestions()), len(linkedin.GetSuggestions()))
	}
	if len(first.GetSuggestions()) != 3 || len(second.GetSuggestions()) != 1 || second.GetNextPageToken() != "" {
		t.Fatalf("pages %d then %d", len(first.GetSuggestions()), len(second.GetSuggestions()))
	}
	requireStatus(t, tokenErr, codes.InvalidArgument, "INVALID_PAGE_TOKEN")
}

func TestAcceptAndDismissOnlyChangeTheState(t *testing.T) {
	h := newHarness(t, true)
	accepted := h.seed(t, h.owner, domain.TargetResume, 0)
	dismissed := h.seed(t, h.owner, domain.TargetResume, 1)

	a, aErr := h.client.AcceptSuggestion(h.ctx(t), &katanav1.AcceptSuggestionRequest{Id: accepted})
	d, dErr := h.client.DismissSuggestion(h.ctx(t), &katanav1.DismissSuggestionRequest{Id: dismissed})

	if aErr != nil || a.GetSuggestion().GetState() != katanav1.SuggestionState_SUGGESTION_STATE_ACCEPTED || a.GetSuggestion().GetDecidedAt() == nil {
		t.Fatalf("accept = %v, %v", a, aErr)
	}
	if dErr != nil || d.GetSuggestion().GetState() != katanav1.SuggestionState_SUGGESTION_STATE_DISMISSED || d.GetSuggestion().GetDecidedAt() == nil {
		t.Fatalf("dismiss = %v, %v", d, dErr)
	}
	if a.GetSuggestion().GetAfter() != "Built a worker pool" || a.GetSuggestion().GetReason() != "merged PR" {
		t.Fatalf("accepting changed the suggestion's content: %+v", a.GetSuggestion())
	}
	var outbox int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox`).Scan(&outbox); err != nil || outbox != 0 {
		t.Fatalf("outbox rows = %d, %v; deciding must emit no event", outbox, err)
	}
}

func TestADecisionIsMadeOnce(t *testing.T) {
	h := newHarness(t, true)
	id := h.seed(t, h.owner, domain.TargetResume, 0)
	if _, err := h.client.AcceptSuggestion(h.ctx(t), &katanav1.AcceptSuggestionRequest{Id: id}); err != nil {
		t.Fatalf("accept: %v", err)
	}

	_, again := h.client.AcceptSuggestion(h.ctx(t), &katanav1.AcceptSuggestionRequest{Id: id})
	_, other := h.client.DismissSuggestion(h.ctx(t), &katanav1.DismissSuggestionRequest{Id: id})

	requireStatus(t, again, codes.FailedPrecondition, "SUGGESTION_ALREADY_DECIDED")
	requireStatus(t, other, codes.FailedPrecondition, "SUGGESTION_ALREADY_DECIDED")
	got, _ := h.client.ListSuggestions(h.ctx(t), &katanav1.ListSuggestionsRequest{})
	if got.GetSuggestions()[0].GetState() != katanav1.SuggestionState_SUGGESTION_STATE_ACCEPTED {
		t.Fatalf("state = %v, want the first decision kept", got.GetSuggestions()[0].GetState())
	}
}

func TestDecidingRefusesUnknownForeignAndMalformedIDs(t *testing.T) {
	h := newHarness(t, true)
	foreign := h.seed(t, uuid.NewString(), domain.TargetResume, 0)

	_, missing := h.client.AcceptSuggestion(h.ctx(t), &katanav1.AcceptSuggestionRequest{Id: uuid.NewString()})
	_, notMine := h.client.DismissSuggestion(h.ctx(t), &katanav1.DismissSuggestionRequest{Id: foreign})
	_, bad := h.client.AcceptSuggestion(h.ctx(t), &katanav1.AcceptSuggestionRequest{Id: "nope"})

	requireStatus(t, missing, codes.NotFound, "SUGGESTION_NOT_FOUND")
	requireStatus(t, notMine, codes.NotFound, "SUGGESTION_NOT_FOUND")
	requireStatus(t, bad, codes.InvalidArgument, "INVALID_ID")
}

func TestSuggestionCallsRequireAnIdentity(t *testing.T) {
	h := newHarness(t, true)
	id := h.seed(t, h.owner, domain.TargetResume, 0)

	_, list := h.client.ListSuggestions(context.Background(), &katanav1.ListSuggestionsRequest{})
	_, accept := h.client.AcceptSuggestion(context.Background(), &katanav1.AcceptSuggestionRequest{Id: id})

	for _, err := range []error{list, accept} {
		if err == nil {
			t.Fatal("a call without an identity succeeded")
		}
	}
}
