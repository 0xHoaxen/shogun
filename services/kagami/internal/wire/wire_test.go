package wire

import (
	"testing"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
)

func TestJobStatusRoundTrip(t *testing.T) {
	for _, s := range []domain.JobStatus{
		domain.JobSaved, domain.JobApplied, domain.JobShortlisted,
		domain.JobInterview, domain.JobOffer, domain.JobRejected,
	} {
		t.Run(string(s), func(t *testing.T) {
			p := JobStatusToProto(s)
			if p == kagamiv1.JobStatus_JOB_STATUS_UNSPECIFIED {
				t.Fatalf("no enum for %s", s)
			}
			back, ok := JobStatusFromProto(p)
			if !ok || back != s {
				t.Fatalf("got %q, %v, want %q", back, ok, s)
			}
		})
	}
}

func TestContactStatusRoundTrip(t *testing.T) {
	for _, s := range []domain.ContactStatus{
		domain.ContactNotReached, domain.ContactReachedOut, domain.ContactConversationStarted,
		domain.ContactReplied, domain.ContactReferralAsked,
	} {
		t.Run(string(s), func(t *testing.T) {
			p := ContactStatusToProto(s)
			if p == kagamiv1.ContactStatus_CONTACT_STATUS_UNSPECIFIED {
				t.Fatalf("no enum for %s", s)
			}
			back, ok := ContactStatusFromProto(p)
			if !ok || back != s {
				t.Fatalf("got %q, %v, want %q", back, ok, s)
			}
		})
	}
}

func TestUnspecifiedAndUnknownStatuses(t *testing.T) {
	tests := []struct {
		name string
		got  func() bool
	}{
		{"job unspecified", func() bool { _, ok := JobStatusFromProto(kagamiv1.JobStatus_JOB_STATUS_UNSPECIFIED); return ok }},
		{"job out of range", func() bool { _, ok := JobStatusFromProto(kagamiv1.JobStatus(99)); return ok }},
		{"contact unspecified", func() bool {
			_, ok := ContactStatusFromProto(kagamiv1.ContactStatus_CONTACT_STATUS_UNSPECIFIED)
			return ok
		}},
		{"contact out of range", func() bool { _, ok := ContactStatusFromProto(kagamiv1.ContactStatus(99)); return ok }},
		{"job unknown domain status", func() bool {
			return JobStatusToProto("hired") != kagamiv1.JobStatus_JOB_STATUS_UNSPECIFIED
		}},
		{"contact unknown domain status", func() bool {
			return ContactStatusToProto("ghosted") != kagamiv1.ContactStatus_CONTACT_STATUS_UNSPECIFIED
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got() {
				t.Fatal("want the conversion to report no match")
			}
		})
	}
}
