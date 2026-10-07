package grpc

import (
	"context"
	"time"

	"github.com/google/uuid"

	shinobiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/shinobi/v1"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
)

// runTimeout bounds a source read started by a call, so a slow source cannot
// hold the call open.
const runTimeout = 90 * time.Second

// Server implements shinobi.v1.ShinobiService.
type Server struct {
	shinobiv1.UnimplementedShinobiServiceServer
	svc *app.Service
}

// New returns a Server that runs its calls on svc.
func New(svc *app.Service) *Server { return &Server{svc: svc} }

// UpsertSource implements shinobi.v1.ShinobiService.
func (s *Server) UpsertSource(ctx context.Context, req *shinobiv1.UpsertSourceRequest) (*shinobiv1.UpsertSourceResponse, error) {
	var id *uuid.UUID
	if raw := req.GetSource().GetId(); raw != "" {
		parsed, err := parseID("source.id", raw)
		if err != nil {
			return nil, toStatus(err)
		}
		id = &parsed
	}
	row, err := s.svc.UpsertSource(ctx, id, sourceInputFromProto(req.GetSource()))
	if err != nil {
		return nil, toStatus(err)
	}
	return &shinobiv1.UpsertSourceResponse{Source: sourceToProto(row)}, nil
}

// ListSources implements shinobi.v1.ShinobiService.
func (s *Server) ListSources(ctx context.Context, _ *shinobiv1.ListSourcesRequest) (*shinobiv1.ListSourcesResponse, error) {
	rows, err := s.svc.ListSources(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*shinobiv1.Source, 0, len(rows))
	for _, row := range rows {
		out = append(out, sourceToProto(row))
	}
	return &shinobiv1.ListSourcesResponse{Sources: out}, nil
}

// RunSource implements shinobi.v1.ShinobiService.
func (s *Server) RunSource(ctx context.Context, req *shinobiv1.RunSourceRequest) (*shinobiv1.RunSourceResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	res, err := s.svc.RunSource(ctx, id)
	if err != nil {
		return nil, toStatus(err)
	}
	return &shinobiv1.RunSourceResponse{Fetched: int32(res.Fetched), Added: int32(res.Added)}, nil //nolint:gosec // bounded by fetch.MaxItems
}
