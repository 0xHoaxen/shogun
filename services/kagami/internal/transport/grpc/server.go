// Package grpc holds the kagami gRPC handlers. They convert requests to use
// case inputs, call internal/app, and map its errors to gRPC status codes.
package grpc

import (
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
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
