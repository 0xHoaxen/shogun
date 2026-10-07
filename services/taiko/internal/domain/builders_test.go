package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBuildersProduceValidNotices(t *testing.T) {
	resets := time.Date(2026, 10, 8, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name      string
		build     func() (Notice, error)
		wantType  Type
		wantTitle string
		wantLink  string
	}{
		{"cover letter ready", func() (Notice, error) { return DraftReady("d1", "cover_letter") }, TypeDraftReady, "Cover letter ready", "/drafts/d1"},
		{"outreach ready", func() (Notice, error) { return DraftReady("d1", "outreach") }, TypeDraftReady, "Outreach message ready", "/drafts/d1"},
		{"unknown kind falls back", func() (Notice, error) { return DraftReady("d1", "mystery") }, TypeDraftReady, "Draft ready", "/drafts/d1"},
		{"draft failed", func() (Notice, error) { return DraftFailed("d1", "generation_failed") }, TypeDraftFailed, "Draft generation failed", "/drafts/d1"},
		{"send failed", func() (Notice, error) { return DraftSendFailed("d1", "auth_revoked") }, TypeDraftSendFailed, "Email was not sent", "/drafts/d1"},
		{"interview", func() (Notice, error) { return Mail(TypeInterviewInvite) }, TypeInterviewInvite, "Interview invite received", "/jobs"},
		{"offer", func() (Notice, error) { return Mail(TypeOffer) }, TypeOffer, "Offer received", "/jobs"},
		{"rejection", func() (Notice, error) { return Mail(TypeRejection) }, TypeRejection, "Application rejected", "/jobs"},
		{"reply", ReplyDetected, TypeReplyDetected, "A contact replied", "/contacts"},
		{"job follow-up", func() (Notice, error) { return FollowUpDue(TargetJob, "2026-10-03") }, TypeFollowUpDue, "Job follow-up due", "/jobs"},
		{"contact follow-up", func() (Notice, error) { return FollowUpDue(TargetContact, "2026-10-03") }, TypeFollowUpDue, "Contact follow-up due", "/contacts"},
		{"threshold", func() (Notice, error) { return BudgetThreshold("overall", "daily", 80) }, TypeBudgetThreshold, "Claude spend at 80% of the daily budget", "/settings/spend"},
		{"exhausted", func() (Notice, error) { return BudgetExhausted("overall", "daily", resets) }, TypeBudgetExhausted, "Claude budget used up", "/settings/spend"},
		{"resume suggestion", func() (Notice, error) { return ProfileSuggestion(SuggestionResume) }, TypeProfileSuggestion, "New resume suggestion", "/profile"},
		{"linkedin suggestion", func() (Notice, error) { return ProfileSuggestion(SuggestionLinkedIn) }, TypeProfileSuggestion, "New LinkedIn suggestion", "/profile"},
		{"match with company", func() (Notice, error) { return DiscoveryMatch("Backend Engineer", "Acme", 0.82) }, TypeDiscoveryMatch, "New job match", "/discovery"},
		{"match without company", func() (Notice, error) { return DiscoveryMatch("Backend Engineer", "", 1) }, TypeDiscoveryMatch, "New job match", "/discovery"},
		{"unnamed suggestion", func() (Notice, error) { return ProfileSuggestion("") }, TypeProfileSuggestion, "New profile suggestion", "/profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.build()
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if got.Type != tt.wantType || got.Title != tt.wantTitle || got.Link != tt.wantLink || got.Body == "" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestBudgetExhaustedBodyStatesTheResetTimeInUTC(t *testing.T) {
	resets := time.Date(2026, 10, 9, 0, 0, 0, 0, time.FixedZone("IST", 5*3600+1800))

	got, err := BudgetExhausted("fude", "monthly", resets)

	want := "The monthly budget for fude is used up. Calls resume 2026-10-08 18:30 UTC."
	if err != nil || got.Body != want {
		t.Fatalf("got %q, %v; want %q", got.Body, err, want)
	}
}

func TestMailRejectsTypesItDoesNotAnnounce(t *testing.T) {
	if _, err := Mail(TypeDraftReady); !errors.Is(err, ErrInvalidType) {
		t.Fatalf("want ErrInvalidType, got %v", err)
	}
}

func TestFollowUpDueRejectsAnUnknownTarget(t *testing.T) {
	if _, err := FollowUpDue("company", "2026-10-03"); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("want ErrInvalidTarget, got %v", err)
	}
}

func TestDiscoveryMatchStatesTheScoreAndCutsLongText(t *testing.T) {
	got, err := DiscoveryMatch("Backend Engineer", "Acme", 0.825)
	long, longErr := DiscoveryMatch(strings.Repeat("é", 500), strings.Repeat("x", 500), 0.7)

	if err != nil || got.Body != "Backend Engineer at Acme scored 83%. Open Discovery to review it." {
		t.Fatalf("body = %q, %v", got.Body, err)
	}
	if longErr != nil || len([]rune(long.Body)) > MaxBodyLength {
		t.Fatalf("long body is %d characters, %v; want it to fit", len([]rune(long.Body)), longErr)
	}
}
