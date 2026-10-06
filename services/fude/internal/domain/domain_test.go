package domain

import (
	"errors"
	"slices"
	"testing"
	"time"
)

var allDraftStates = []DraftState{
	DraftGenerating, DraftPending, DraftApproved, DraftSent, DraftDiscarded, DraftFailed,
}

func TestDraftTransitions(t *testing.T) {
	allowed := map[DraftState][]DraftState{
		DraftGenerating: {DraftPending, DraftFailed},
		DraftFailed:     {DraftGenerating},
		DraftPending:    {DraftPending, DraftApproved, DraftDiscarded},
		DraftApproved:   {DraftSent, DraftPending},
	}
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)

	for _, from := range allDraftStates {
		for _, to := range allDraftStates {
			want := slices.Contains(allowed[from], to)
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				got, err := Draft{State: from, Version: 2}.Move(to, now)
				if want {
					if err != nil || got.State != to || !got.UpdatedAt.Equal(now) {
						t.Fatalf("want move to %s, got %+v, %v", to, got, err)
					}
					return
				}
				var te *TransitionError
				if !errors.As(err, &te) || te.Reason != ReasonDraftStateInvalidTransition {
					t.Fatalf("want %s, got %v", ReasonDraftStateInvalidTransition, err)
				}
				if got.State != from {
					t.Fatalf("state changed on a rejected move: %s", got.State)
				}
			})
		}
	}
}

func TestDraftStateValid(t *testing.T) {
	for _, s := range allDraftStates {
		if !s.Valid() {
			t.Errorf("%s should be valid", s)
		}
	}
	if DraftState("bogus").Valid() {
		t.Error("bogus state should be invalid")
	}
}

func TestDraftGenerated(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)

	got, err := Draft{State: DraftGenerating, FailureReason: "old"}.Generated(1, now)

	if err != nil || got.State != DraftPending || got.CurrentVersion != 1 || got.FailureReason != "" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := (Draft{State: DraftPending}).Generated(2, now); err == nil {
		t.Fatal("generating finish on a pending draft should fail")
	}
}

func TestDraftGenerationFailedAndRetry(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)

	failed, err := Draft{State: DraftGenerating}.GenerationFailed("llm down", now)
	if err != nil || failed.State != DraftFailed || failed.FailureReason != "llm down" {
		t.Fatalf("got %+v, %v", failed, err)
	}
	retried, err := failed.Retry(now)
	if err != nil || retried.State != DraftGenerating || retried.FailureReason != "" {
		t.Fatalf("got %+v, %v", retried, err)
	}
	if _, err := (Draft{State: DraftPending}).Retry(now); err == nil {
		t.Fatal("retry of a pending draft should fail")
	}
}

func TestDraftNewVersion(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		from    DraftState
		wantErr bool
	}{
		{"regenerate or edit of a pending draft", DraftPending, false},
		{"edit after approval returns to pending", DraftApproved, false},
		{"edit of a sent draft", DraftSent, true},
		{"edit of a discarded draft", DraftDiscarded, true},
		{"edit while generating", DraftGenerating, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Draft{State: tt.from, CurrentVersion: 2}.NewVersion(3, now)

			if tt.wantErr {
				if err == nil || got.CurrentVersion != 2 {
					t.Fatalf("want error and no change, got %+v, %v", got, err)
				}
				return
			}
			if err != nil || got.State != DraftPending || got.CurrentVersion != 3 {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestDraftCanApprove(t *testing.T) {
	tests := []struct {
		name    string
		draft   Draft
		version int32
		want    bool
	}{
		{"newest version of a pending draft", Draft{State: DraftPending, CurrentVersion: 3}, 3, true},
		{"stale version", Draft{State: DraftPending, CurrentVersion: 3}, 2, false},
		{"already approved", Draft{State: DraftApproved, CurrentVersion: 3}, 3, false},
		{"still generating", Draft{State: DraftGenerating}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.draft.CanApprove(tt.version); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDraftRegenerateOnlyWhenPending(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	for _, from := range allDraftStates {
		t.Run(string(from), func(t *testing.T) {
			got, err := Draft{State: from, CurrentVersion: 2}.Regenerate(now)

			if from == DraftPending {
				if err != nil || got.State != DraftPending || got.CurrentVersion != 2 {
					t.Fatalf("got %+v, %v", got, err)
				}
				return
			}
			var te *TransitionError
			if !errors.As(err, &te) {
				t.Fatalf("want TransitionError, got %v", err)
			}
		})
	}
}

func TestDraftRecordSent(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		draft       Draft
		version     int32
		wantState   DraftState
		wantChanged bool
		wantErr     bool
	}{
		{"approved becomes sent", Draft{State: DraftApproved, CurrentVersion: 2}, 2, DraftSent, true, false},
		{"pending on the same version becomes sent: the mail went out after all", Draft{State: DraftPending, CurrentVersion: 2}, 2, DraftSent, true, false},
		{"already sent is left alone", Draft{State: DraftSent, CurrentVersion: 2}, 2, DraftSent, false, false},
		{"a report for an older version is ignored", Draft{State: DraftApproved, CurrentVersion: 3}, 2, DraftApproved, false, false},
		{"a pending draft on a newer version is ignored", Draft{State: DraftPending, CurrentVersion: 3}, 2, DraftPending, false, false},
		{"a discarded draft cannot be sent", Draft{State: DraftDiscarded, CurrentVersion: 2}, 2, DraftDiscarded, false, true},
		{"a generating draft cannot be sent", Draft{State: DraftGenerating, CurrentVersion: 2}, 2, DraftGenerating, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, err := tt.draft.RecordSent(tt.version, now)

			if (err != nil) != tt.wantErr || changed != tt.wantChanged || got.State != tt.wantState {
				t.Fatalf("got %s changed %v err %v", got.State, changed, err)
			}
			if changed && !got.UpdatedAt.Equal(now) {
				t.Fatalf("updated at %v", got.UpdatedAt)
			}
		})
	}
}

func TestDraftRecordSendFailed(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		draft       Draft
		version     int32
		wantState   DraftState
		wantChanged bool
	}{
		{"approved goes back to pending", Draft{State: DraftApproved, CurrentVersion: 2}, 2, DraftPending, true},
		{"a stale report for an older version is ignored", Draft{State: DraftApproved, CurrentVersion: 3}, 2, DraftApproved, false},
		{"a draft already edited back to pending is left alone", Draft{State: DraftPending, CurrentVersion: 3}, 2, DraftPending, false},
		{"a sent draft is not undone", Draft{State: DraftSent, CurrentVersion: 2}, 2, DraftSent, false},
		{"a discarded draft is left alone", Draft{State: DraftDiscarded, CurrentVersion: 2}, 2, DraftDiscarded, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, err := tt.draft.RecordSendFailed(tt.version, now)

			if err != nil || changed != tt.wantChanged || got.State != tt.wantState {
				t.Fatalf("got %s changed %v err %v", got.State, changed, err)
			}
		})
	}
}
