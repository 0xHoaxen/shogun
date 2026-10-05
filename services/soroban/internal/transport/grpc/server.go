// Package grpc holds the soroban gRPC handlers. They convert requests to use
// case inputs, call internal/app, and map its errors to gRPC status codes.
package grpc

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/soroban/internal/app"
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
