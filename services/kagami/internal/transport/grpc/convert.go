package grpc

import (
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
	"github.com/0xHoaxen/shogun/services/kagami/internal/wire"
)

const dateLayout = "2006-01-02"

// companyNames maps company ids to names for the jobs and contacts of a response.
type companyNames map[uuid.UUID]string

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func dateString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(dateLayout)
}

func optionalTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func companyToProto(c db.Company) *kagamiv1.Company {
	return &kagamiv1.Company{
		Id: c.ID.String(), Name: c.Name, Domain: deref(c.Domain), Notes: deref(c.Notes),
		Version: c.Version, CreatedAt: timestamppb.New(c.CreatedAt), UpdatedAt: timestamppb.New(c.UpdatedAt),
	}
}

func jobToProto(j db.Job, names companyNames) *kagamiv1.Job {
	return &kagamiv1.Job{
		Id: j.ID.String(), CompanyId: j.CompanyID.String(), Title: j.Title, Url: deref(j.Url),
		Source: j.Source, Status: wire.JobStatusToProto(domain.JobStatus(j.Status)),
		AppliedOn: dateString(j.AppliedOn), NextFollowUp: dateString(j.NextFollowUp),
		SalaryText: deref(j.SalaryText), Location: deref(j.Location), Description: deref(j.Description),
		Version: j.Version, CreatedAt: timestamppb.New(j.CreatedAt), UpdatedAt: timestamppb.New(j.UpdatedAt),
		ArchivedAt: optionalTimestamp(j.ArchivedAt), CompanyName: names[j.CompanyID],
	}
}

func contactToProto(c db.Contact, names companyNames) *kagamiv1.Contact {
	var companyID, companyName, jobID string
	if c.CompanyID != nil {
		companyID = c.CompanyID.String()
		companyName = names[*c.CompanyID]
	}
	if c.JobID != nil {
		jobID = c.JobID.String()
	}
	return &kagamiv1.Contact{
		Id: c.ID.String(), FullName: c.FullName, CompanyId: companyID, Role: deref(c.Role),
		Email: deref(c.Email), LinkedinUrl: deref(c.LinkedinUrl), XHandle: deref(c.XHandle), Phone: deref(c.Phone),
		Relationship: deref(c.Relationship), HowWeMet: deref(c.HowWeMet),
		Status:           wire.ContactStatusToProto(domain.ContactStatus(c.Status)),
		PreferredChannel: deref(c.PreferredChannel), LastContacted: dateString(c.LastContacted),
		NextFollowUp: dateString(c.NextFollowUp), TargetRole: deref(c.TargetRole), JobId: jobID,
		Tags: c.Tags, Notes: deref(c.Notes), Version: c.Version,
		CreatedAt: timestamppb.New(c.CreatedAt), UpdatedAt: timestamppb.New(c.UpdatedAt),
		ArchivedAt: optionalTimestamp(c.ArchivedAt), CompanyName: companyName,
	}
}

func contactEventToProto(e db.ContactEvent) *kagamiv1.ContactEvent {
	return &kagamiv1.ContactEvent{
		Id: e.ID.String(), ContactId: e.ContactID.String(), Kind: e.Kind, Channel: deref(e.Channel),
		Payload: string(e.Payload), OccurredAt: timestamppb.New(e.OccurredAt),
	}
}

func jobEventToProto(e db.JobEvent) *kagamiv1.JobEvent {
	return &kagamiv1.JobEvent{
		Id: e.ID.String(), JobId: e.JobID.String(), Kind: e.Kind,
		FromStatus: wire.JobStatusToProto(domain.JobStatus(deref(e.FromStatus))),
		ToStatus:   wire.JobStatusToProto(domain.JobStatus(deref(e.ToStatus))),
		Payload:    string(e.Payload), OccurredAt: timestamppb.New(e.OccurredAt),
	}
}
