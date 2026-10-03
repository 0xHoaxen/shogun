package grpc

import (
	"context"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/wire"
)

// optionalContactStatus reads a status filter or default: unspecified means none.
func optionalContactStatus(p kagamiv1.ContactStatus) (domain.ContactStatus, error) {
	if p == kagamiv1.ContactStatus_CONTACT_STATUS_UNSPECIFIED {
		return "", nil
	}
	return requiredContactStatus(p)
}

func requiredContactStatus(p kagamiv1.ContactStatus) (domain.ContactStatus, error) {
	s, ok := wire.ContactStatusFromProto(p)
	if !ok {
		return "", &app.InvalidArgumentError{Reason: "INVALID_STATUS", Msg: "status is missing or unknown"}
	}
	return s, nil
}

func contactFieldsFromProto(c *kagamiv1.Contact) app.ContactFields {
	return app.ContactFields{
		FullName: c.GetFullName(), CompanyID: c.GetCompanyId(), Role: c.GetRole(), Email: c.GetEmail(),
		LinkedinURL: c.GetLinkedinUrl(), XHandle: c.GetXHandle(), Phone: c.GetPhone(),
		Relationship: c.GetRelationship(), HowWeMet: c.GetHowWeMet(), PreferredChannel: c.GetPreferredChannel(),
		LastContacted: c.GetLastContacted(), NextFollowUp: c.GetNextFollowUp(), TargetRole: c.GetTargetRole(),
		JobID: c.GetJobId(), Tags: c.GetTags(), Notes: c.GetNotes(),
	}
}

// AddContact implements kagami.v1.KagamiService.
func (s *Server) AddContact(ctx context.Context, req *kagamiv1.AddContactRequest) (*kagamiv1.AddContactResponse, error) {
	status, err := optionalContactStatus(req.GetContact().GetStatus())
	if err != nil {
		return nil, toStatus(err)
	}
	contact, err := s.svc.AddContact(ctx, app.AddContactInput{
		ContactFields:  contactFieldsFromProto(req.GetContact()),
		Status:         status,
		CompanyName:    req.GetCompanyName(),
		IdempotencyKey: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &kagamiv1.AddContactResponse{Contact: contactToProto(contact)}, nil
}

// UpdateContact implements kagami.v1.KagamiService.
func (s *Server) UpdateContact(ctx context.Context, req *kagamiv1.UpdateContactRequest) (*kagamiv1.UpdateContactResponse, error) {
	contact, err := s.svc.UpdateContact(ctx, app.UpdateContactInput{
		ID:      req.GetContact().GetId(),
		Version: req.GetVersion(),
		Paths:   req.GetUpdateMask().GetPaths(),
		Fields:  contactFieldsFromProto(req.GetContact()),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &kagamiv1.UpdateContactResponse{Contact: contactToProto(contact)}, nil
}

// ListContacts implements kagami.v1.KagamiService.
func (s *Server) ListContacts(ctx context.Context, req *kagamiv1.ListContactsRequest) (*kagamiv1.ListContactsResponse, error) {
	status, err := optionalContactStatus(req.GetStatus())
	if err != nil {
		return nil, toStatus(err)
	}
	res, err := s.svc.ListContacts(ctx, app.ListContactsInput{
		Status: status, Tag: req.GetTag(), CompanyID: req.GetCompanyId(), Query: req.GetQuery(),
		PageSize: req.GetPageSize(), PageToken: req.GetPageToken(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	contacts := make([]*kagamiv1.Contact, 0, len(res.Contacts))
	for _, c := range res.Contacts {
		contacts = append(contacts, contactToProto(c))
	}
	return &kagamiv1.ListContactsResponse{Contacts: contacts, NextPageToken: res.NextPageToken}, nil
}

// GetContact implements kagami.v1.KagamiService.
func (s *Server) GetContact(ctx context.Context, req *kagamiv1.GetContactRequest) (*kagamiv1.GetContactResponse, error) {
	detail, err := s.svc.GetContact(ctx, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	timeline := make([]*kagamiv1.ContactEvent, 0, len(detail.Timeline))
	for _, e := range detail.Timeline {
		timeline = append(timeline, contactEventToProto(e))
	}
	return &kagamiv1.GetContactResponse{Contact: contactToProto(detail.Contact), Timeline: timeline}, nil
}

// ChangeContactStatus implements kagami.v1.KagamiService.
func (s *Server) ChangeContactStatus(ctx context.Context, req *kagamiv1.ChangeContactStatusRequest) (*kagamiv1.ChangeContactStatusResponse, error) {
	to, err := requiredContactStatus(req.GetToStatus())
	if err != nil {
		return nil, toStatus(err)
	}
	contact, err := s.svc.ChangeContactStatus(ctx, app.ChangeContactStatusInput{
		ID: req.GetId(), To: to, Version: req.GetVersion(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &kagamiv1.ChangeContactStatusResponse{Contact: contactToProto(contact)}, nil
}
