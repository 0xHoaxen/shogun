package app

import (
	"errors"
	"slices"
	"testing"
)

func TestParseContactFieldsNormalizes(t *testing.T) {
	v, err := parseContactFields(ContactFields{
		FullName: "  Priya Raman ", Email: "priya@lumen.example", Relationship: "Recruiter",
		PreferredChannel: " LinkedIn ", Tags: []string{"Go", " go ", "referral", ""},
		LastContacted: "2026-09-29", Role: "  ",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.FullName != "Priya Raman" || *v.Relationship != "recruiter" || *v.PreferredChannel != "linkedin" {
		t.Fatalf("got %+v", v)
	}
	if !slices.Equal(v.Tags, []string{"go", "referral"}) {
		t.Fatalf("got tags %v, want [go referral]", v.Tags)
	}
	if v.Role != nil || v.NextFollowUp != nil || v.LastContacted == nil {
		t.Fatalf("blank text must be nil and dates parsed: %+v", v)
	}
}

func TestParseContactFieldsRejects(t *testing.T) {
	tests := []struct {
		name       string
		fields     ContactFields
		wantReason string
	}{
		{"no name", ContactFields{}, "FULL_NAME_REQUIRED"},
		{"email with a display name", ContactFields{FullName: "A", Email: "Priya <priya@lumen.example>"}, "INVALID_EMAIL"},
		{"not an email", ContactFields{FullName: "A", Email: "priya"}, "INVALID_EMAIL"},
		{"linkedin without a scheme", ContactFields{FullName: "A", LinkedinURL: "linkedin.example/in/a"}, "INVALID_URL"},
		{"unknown relationship", ContactFields{FullName: "A", Relationship: "rival"}, "INVALID_RELATIONSHIP"},
		{"unknown channel", ContactFields{FullName: "A", PreferredChannel: "fax"}, "INVALID_PREFERRED_CHANNEL"},
		{"bad date", ContactFields{FullName: "A", NextFollowUp: "tomorrow"}, "INVALID_DATE"},
		{"bad company id", ContactFields{FullName: "A", CompanyID: "x"}, "INVALID_ID"},
		{"bad job id", ContactFields{FullName: "A", JobID: "x"}, "INVALID_ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseContactFields(tt.fields)
			var ia *InvalidArgumentError
			if !errors.As(err, &ia) || ia.Reason != tt.wantReason {
				t.Fatalf("got %v, want reason %s", err, tt.wantReason)
			}
		})
	}
}

func TestOverlayContactFields(t *testing.T) {
	base := ContactFields{FullName: "Kenji", Role: "Tech Lead", Tags: []string{"go"}}
	req := ContactFields{FullName: "Kenji W.", Role: "ignored", Tags: []string{"referral"}}

	got, err := overlayContactFields(base, []string{"full_name", "tags"}, req)
	if err != nil {
		t.Fatalf("overlay: %v", err)
	}
	if got.FullName != "Kenji W." || got.Role != "Tech Lead" || !slices.Equal(got.Tags, []string{"referral"}) {
		t.Fatalf("got %+v", got)
	}
	if base.FullName != "Kenji" || !slices.Equal(base.Tags, []string{"go"}) {
		t.Fatalf("base was modified: %+v", base)
	}
}

func TestOverlayContactFieldsRejects(t *testing.T) {
	tests := []struct {
		name       string
		paths      []string
		wantReason string
	}{
		{"empty mask", nil, "UPDATE_MASK_REQUIRED"},
		{"status", []string{"status"}, "STATUS_NOT_EDITABLE"},
		{"unknown", []string{"idempotency_key"}, "UNKNOWN_FIELD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := overlayContactFields(ContactFields{}, tt.paths, ContactFields{})
			var ia *InvalidArgumentError
			if !errors.As(err, &ia) || ia.Reason != tt.wantReason {
				t.Fatalf("got %v, want reason %s", err, tt.wantReason)
			}
		})
	}
}

func TestOverlayCoversEveryEditableField(t *testing.T) {
	req := ContactFields{
		FullName: "n", CompanyID: "c", Role: "r", Email: "e", LinkedinURL: "l", XHandle: "x", Phone: "p",
		Relationship: "rel", HowWeMet: "h", PreferredChannel: "ch", LastContacted: "lc", NextFollowUp: "nf",
		TargetRole: "tr", JobID: "j", Tags: []string{"t"}, Notes: "no",
	}
	paths := []string{
		"full_name", "company_id", "role", "email", "linkedin_url", "x_handle", "phone", "relationship",
		"how_we_met", "preferred_channel", "last_contacted", "next_follow_up", "target_role", "job_id", "tags", "notes",
	}

	got, err := overlayContactFields(ContactFields{}, paths, req)
	if err != nil {
		t.Fatalf("overlay: %v", err)
	}
	if got.FullName != req.FullName || got.Notes != req.Notes || got.JobID != req.JobID ||
		got.PreferredChannel != req.PreferredChannel || !slices.Equal(got.Tags, req.Tags) {
		t.Fatalf("got %+v, want %+v", got, req)
	}
}
