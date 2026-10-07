package grpc

import (
	"context"

	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
	"github.com/0xHoaxen/shogun/services/katana/internal/wire"
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

// ListSuggestions implements katana.v1.KatanaService.
func (s *Server) ListSuggestions(ctx context.Context, req *katanav1.ListSuggestionsRequest) (*katanav1.ListSuggestionsResponse, error) {
	var filter store.SuggestionFilter
	if state := wire.StateFromProto(req.GetState()); state != "" {
		filter.State = &state
	}
	if target := wire.TargetFromProto(req.GetTarget()); target != "" {
		filter.Target = &target
	}
	page, err := s.svc.ListSuggestions(ctx, filter, store.Page{Size: req.GetPageSize(), Token: req.GetPageToken()})
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*katanav1.Suggestion, 0, len(page.Suggestions))
	for _, row := range page.Suggestions {
		out = append(out, suggestionToProto(row))
	}
	return &katanav1.ListSuggestionsResponse{Suggestions: out, NextPageToken: page.NextPageToken}, nil
}

// AcceptSuggestion implements katana.v1.KatanaService.
func (s *Server) AcceptSuggestion(ctx context.Context, req *katanav1.AcceptSuggestionRequest) (*katanav1.AcceptSuggestionResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	row, err := s.svc.AcceptSuggestion(ctx, id)
	if err != nil {
		return nil, toStatus(err)
	}
	return &katanav1.AcceptSuggestionResponse{Suggestion: suggestionToProto(row)}, nil
}

// DismissSuggestion implements katana.v1.KatanaService.
func (s *Server) DismissSuggestion(ctx context.Context, req *katanav1.DismissSuggestionRequest) (*katanav1.DismissSuggestionResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	row, err := s.svc.DismissSuggestion(ctx, id)
	if err != nil {
		return nil, toStatus(err)
	}
	return &katanav1.DismissSuggestionResponse{Suggestion: suggestionToProto(row)}, nil
}
