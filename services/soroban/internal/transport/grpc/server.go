// Package grpc holds the soroban gRPC handlers. They convert requests to use
// case inputs, call internal/app, and map its errors to gRPC status codes.
package grpc

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/soroban/internal/app"
	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
)

// Server implements soroban.v1.SorobanService. RPCs without a handler yet
// answer Unimplemented.
type Server struct {
	sorobanv1.UnimplementedSorobanServiceServer
	svc *app.Service
}

// New returns a Server that runs its calls on svc.
func New(svc *app.Service) *Server {
	return &Server{svc: svc}
}

// Reserve implements soroban.v1.SorobanService.
func (s *Server) Reserve(ctx context.Context, req *sorobanv1.ReserveRequest) (*sorobanv1.ReserveResponse, error) {
	res, err := s.svc.Reserve(ctx, app.ReserveInput{
		Service:         req.GetService(),
		Feature:         req.GetFeature(),
		Model:           req.GetModel(),
		EstInputTokens:  req.GetEstInputTokens(),
		EstOutputTokens: req.GetEstOutputTokens(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &sorobanv1.ReserveResponse{
		ReservationId: res.ReservationID.String(),
		ExpiresAt:     timestamppb.New(res.ExpiresAt),
		EstMicros:     res.EstMicros,
	}, nil
}

// Commit implements soroban.v1.SorobanService.
func (s *Server) Commit(ctx context.Context, req *sorobanv1.CommitRequest) (*sorobanv1.CommitResponse, error) {
	usage := req.GetUsage()
	entry, err := s.svc.Commit(ctx, app.CommitInput{
		ReservationID: req.GetReservationId(),
		Usage: domain.Usage{
			InputTokens:      usage.GetInputTokens(),
			OutputTokens:     usage.GetOutputTokens(),
			CacheReadTokens:  usage.GetCacheReadTokens(),
			CacheWriteTokens: usage.GetCacheWriteTokens(),
		},
		RequestID: req.GetRequestId(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &sorobanv1.CommitResponse{Entry: ledgerToProto(entry)}, nil
}

// Release implements soroban.v1.SorobanService.
func (s *Server) Release(ctx context.Context, req *sorobanv1.ReleaseRequest) (*sorobanv1.ReleaseResponse, error) {
	if err := s.svc.Release(ctx, req.GetReservationId()); err != nil {
		return nil, toStatus(err)
	}
	return &sorobanv1.ReleaseResponse{}, nil
}
