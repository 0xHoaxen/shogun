package grpc

import (
	"context"

	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
)

// Server implements katana.v1.KatanaService.
type Server struct {
	katanav1.UnimplementedKatanaServiceServer
	svc *app.Service
}

// New returns a Server that runs its calls on svc.
func New(svc *app.Service) *Server { return &Server{svc: svc} }

// SyncGitHub implements katana.v1.KatanaService.
func (s *Server) SyncGitHub(ctx context.Context, _ *katanav1.SyncGitHubRequest) (*katanav1.SyncGitHubResponse, error) {
	res, err := s.svc.SyncGitHub(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	id := ""
	if res.Changed {
		id = res.SnapshotID.String()
	}
	return &katanav1.SyncGitHubResponse{Changed: res.Changed, SnapshotId: id}, nil
}
