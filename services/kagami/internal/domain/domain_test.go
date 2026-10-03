package domain

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestJobTransitions(t *testing.T) {
	all := []JobStatus{JobSaved, JobApplied, JobShortlisted, JobInterview, JobOffer, JobRejected}
	allowed := map[JobStatus][]JobStatus{
		JobSaved:       {JobApplied, JobRejected},
		JobApplied:     {JobShortlisted, JobInterview, JobRejected, JobOffer},
		JobShortlisted: {JobInterview, JobRejected, JobOffer},
		JobInterview:   {JobInterview, JobOffer, JobRejected},
		JobOffer:       {JobRejected},
		JobRejected:    {JobApplied},
	}
	now := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)

	for _, from := range all {
		for _, to := range all {
			want := slices.Contains(allowed[from], to)
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				got, err := Job{Status: from, Version: 3}.ChangeStatus(to, now)
				if want {
					if err != nil || got.Status != to || !got.UpdatedAt.Equal(now) {
						t.Fatalf("want move to %s, got %+v, %v", to, got, err)
					}
					return
				}
				var te *TransitionError
				if !errors.As(err, &te) || te.Reason != ReasonJobStatusInvalidTransition {
					t.Fatalf("want %s, got %v", ReasonJobStatusInvalidTransition, err)
				}
				if got.Status != from {
					t.Fatalf("status changed on a rejected move: %s", got.Status)
				}
			})
		}
	}
}

func TestContactTransitions(t *testing.T) {
	all := []ContactStatus{
		ContactNotReached, ContactReachedOut, ContactConversationStarted,
		ContactReplied, ContactReferralAsked,
	}
	allowed := map[ContactStatus][]ContactStatus{
		ContactNotReached:          {ContactReachedOut},
		ContactReachedOut:          {ContactConversationStarted, ContactReplied},
		ContactReplied:             {ContactConversationStarted, ContactReferralAsked},
		ContactConversationStarted: {ContactReferralAsked, ContactReplied},
		ContactReferralAsked:       {ContactReplied, ContactConversationStarted},
	}
	now := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)

	for _, from := range all {
		for _, to := range all {
			want := slices.Contains(allowed[from], to)
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				got, err := Contact{Status: from}.ChangeStatus(to, now)
				if want {
					if err != nil || got.Status != to || !got.UpdatedAt.Equal(now) {
						t.Fatalf("want move to %s, got %+v, %v", to, got, err)
					}
					return
				}
				var te *TransitionError
				if !errors.As(err, &te) || te.Reason != ReasonContactStatusInvalidTransition {
					t.Fatalf("want %s, got %v", ReasonContactStatusInvalidTransition, err)
				}
			})
		}
	}
}

func TestStatusValid(t *testing.T) {
	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{"known job status", JobStatus("offer").Valid(), true},
		{"unknown job status", JobStatus("hired").Valid(), false},
		{"empty job status", JobStatus("").Valid(), false},
		{"known contact status", ContactStatus("replied").Valid(), true},
		{"unknown contact status", ContactStatus("ghosted").Valid(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}
