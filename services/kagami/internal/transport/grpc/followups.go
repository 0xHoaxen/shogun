package grpc

import (
	"context"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

// ListDueFollowUps implements kagami.v1.KagamiService.
func (s *Server) ListDueFollowUps(ctx context.Context, req *kagamiv1.ListDueFollowUpsRequest) (*kagamiv1.ListDueFollowUpsResponse, error) {
	due, err := s.svc.DueFollowUps(ctx, req.GetOnOrBefore())
	if err != nil {
		return nil, toStatus(err)
	}
	jobs := make([]*kagamiv1.Job, 0, len(due.Jobs))
	for _, j := range due.Jobs {
		jobs = append(jobs, jobToProto(j))
	}
	contacts := make([]*kagamiv1.Contact, 0, len(due.Contacts))
	for _, c := range due.Contacts {
		contacts = append(contacts, contactToProto(c))
	}
	return &kagamiv1.ListDueFollowUpsResponse{Jobs: jobs, Contacts: contacts}, nil
}
