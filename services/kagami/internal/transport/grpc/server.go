// Package grpc holds the kagami gRPC handlers. They convert requests to use
// case inputs, call internal/app, and map its errors to gRPC status codes.
package grpc

import (
	"context"

	"github.com/google/uuid"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

// Server implements kagami.v1.KagamiService. RPCs without a handler yet
// answer Unimplemented.
type Server struct {
	kagamiv1.UnimplementedKagamiServiceServer
	svc *app.Service
}

// New returns a Server that runs its calls on svc.
func New(svc *app.Service) *Server {
	return &Server{svc: svc}
}

// namesFor looks up, in one query, the company names of the jobs and contacts
// of a response.
func (s *Server) namesFor(ctx context.Context, jobs []db.Job, contacts []db.Contact) (companyNames, error) {
	ids := make([]uuid.UUID, 0, len(jobs)+len(contacts))
	for _, j := range jobs {
		ids = append(ids, j.CompanyID)
	}
	for _, c := range contacts {
		if c.CompanyID != nil {
			ids = append(ids, *c.CompanyID)
		}
	}
	names, err := s.svc.CompanyNames(ctx, ids)
	return companyNames(names), err
}

// jobProto converts one job, with its company name.
func (s *Server) jobProto(ctx context.Context, j db.Job) (*kagamiv1.Job, error) {
	names, err := s.namesFor(ctx, []db.Job{j}, nil)
	if err != nil {
		return nil, err
	}
	return jobToProto(j, names), nil
}

// contactProto converts one contact, with its company name.
func (s *Server) contactProto(ctx context.Context, c db.Contact) (*kagamiv1.Contact, error) {
	names, err := s.namesFor(ctx, nil, []db.Contact{c})
	if err != nil {
		return nil, err
	}
	return contactToProto(c, names), nil
}
