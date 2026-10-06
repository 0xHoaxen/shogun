package grpc

import (
	"context"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

// FindMailLinks implements kagami.v1.KagamiService.
func (s *Server) FindMailLinks(ctx context.Context, req *kagamiv1.FindMailLinksRequest) (*kagamiv1.FindMailLinksResponse, error) {
	links, err := s.svc.FindMailLinks(ctx, req.GetFromEmail(), req.GetUrls())
	if err != nil {
		return nil, toStatus(err)
	}
	return &kagamiv1.FindMailLinksResponse{ContactId: links.ContactID, JobId: links.JobID}, nil
}
