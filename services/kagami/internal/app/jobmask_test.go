package app

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

func sampleJob() db.Job {
	url := "https://northwind.example/jobs/1"
	due := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	return db.Job{
		ID: uuid.New(), OwnerID: uuid.New(), Title: "Backend Engineer", Source: "manual",
		Url: &url, NextFollowUp: &due, Version: 4,
	}
}

func TestApplyJobMask(t *testing.T) {
	tests := []struct {
		name         string
		paths        []string
		fields       JobFields
		wantTitle    string
		wantURLNil   bool
		wantFollowUp string
		wantChanged  bool
	}{
		{"title only leaves the rest", []string{"title"}, JobFields{Title: "Senior Backend Engineer"}, "Senior Backend Engineer", false, "2026-10-03", false},
		{"unmasked fields are ignored", []string{"title"}, JobFields{Title: "X", URL: "https://other.example"}, "X", false, "2026-10-03", false},
		{"empty optional clears the column", []string{"url"}, JobFields{}, "Backend Engineer", true, "2026-10-03", false},
		{"new follow-up date", []string{"next_follow_up"}, JobFields{NextFollowUp: "2026-10-10"}, "Backend Engineer", false, "2026-10-10", true},
		{"same follow-up date is no change", []string{"next_follow_up"}, JobFields{NextFollowUp: "2026-10-03"}, "Backend Engineer", false, "2026-10-03", false},
		{"cleared follow-up date", []string{"next_follow_up"}, JobFields{}, "Backend Engineer", false, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := sampleJob()
			got, err := applyJobMask(current, tt.paths, tt.fields)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if got.params.Title != tt.wantTitle {
				t.Errorf("title %q, want %q", got.params.Title, tt.wantTitle)
			}
			if (got.params.Url == nil) != tt.wantURLNil {
				t.Errorf("url nil = %v, want %v", got.params.Url == nil, tt.wantURLNil)
			}
			gotDue := ""
			if got.params.NextFollowUp != nil {
				gotDue = got.params.NextFollowUp.Format(dateLayout)
			}
			if gotDue != tt.wantFollowUp || got.followUpChanged != tt.wantChanged {
				t.Errorf("follow-up %q changed=%v, want %q changed=%v", gotDue, got.followUpChanged, tt.wantFollowUp, tt.wantChanged)
			}
			if got.params.Version != current.Version || got.params.ID != current.ID {
				t.Errorf("version or id not carried over: %+v", got.params)
			}
		})
	}
}

func TestApplyJobMaskDoesNotModifyTheJob(t *testing.T) {
	current := sampleJob()
	_, err := applyJobMask(current, []string{"title", "url"}, JobFields{Title: "Changed"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if current.Title != "Backend Engineer" || current.Url == nil {
		t.Fatalf("current job was modified: %+v", current)
	}
}

func TestApplyJobMaskRejects(t *testing.T) {
	tests := []struct {
		name       string
		paths      []string
		fields     JobFields
		wantReason string
	}{
		{"empty mask", nil, JobFields{}, "UPDATE_MASK_REQUIRED"},
		{"status", []string{"status"}, JobFields{}, "STATUS_NOT_EDITABLE"},
		{"unknown field", []string{"company_id"}, JobFields{}, "UNKNOWN_FIELD"},
		{"empty title", []string{"title"}, JobFields{}, "TITLE_REQUIRED"},
		{"bad source", []string{"source"}, JobFields{Source: "linkedin"}, "INVALID_SOURCE"},
		{"bad date", []string{"applied_on"}, JobFields{AppliedOn: "03/10/2026"}, "INVALID_DATE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := applyJobMask(sampleJob(), tt.paths, tt.fields)
			var ia *InvalidArgumentError
			if !errors.As(err, &ia) || ia.Reason != tt.wantReason {
				t.Fatalf("got %v, want reason %s", err, tt.wantReason)
			}
		})
	}
}
