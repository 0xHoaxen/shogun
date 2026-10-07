package connectapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/grpc"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
)

// ProfileBackend is the part of katana's client ProfileServer uses.
type ProfileBackend interface {
	SyncGitHub(ctx context.Context, in *katanav1.SyncGitHubRequest, opts ...grpc.CallOption) (*katanav1.SyncGitHubResponse, error)
	ListSuggestions(ctx context.Context, in *katanav1.ListSuggestionsRequest, opts ...grpc.CallOption) (*katanav1.ListSuggestionsResponse, error)
	AcceptSuggestion(ctx context.Context, in *katanav1.AcceptSuggestionRequest, opts ...grpc.CallOption) (*katanav1.AcceptSuggestionResponse, error)
	DismissSuggestion(ctx context.Context, in *katanav1.DismissSuggestionRequest, opts ...grpc.CallOption) (*katanav1.DismissSuggestionResponse, error)
}

// ProfileServer implements shogun.api.v1.ProfileService on top of katana.
type ProfileServer struct {
	katana ProfileBackend
	log    *slog.Logger
}

var _ apiv1connect.ProfileServiceHandler = (*ProfileServer)(nil)

// NewProfileServer returns a ProfileServer that calls katana through backend.
func NewProfileServer(backend ProfileBackend, log *slog.Logger) *ProfileServer {
	return &ProfileServer{katana: backend, log: log}
}

// The browser API and katana name their enum values alike, so they are
// converted by name: a value one side lacks becomes unspecified instead of
// silently turning into a different one.

func suggestionTargetToAPI(t katanav1.SuggestionTarget) apiv1.SuggestionTarget {
	return apiv1.SuggestionTarget(apiv1.SuggestionTarget_value[t.String()])
}

func suggestionTargetToKatana(t apiv1.SuggestionTarget) katanav1.SuggestionTarget {
	return katanav1.SuggestionTarget(katanav1.SuggestionTarget_value[t.String()])
}

func suggestionStateToAPI(s katanav1.SuggestionState) apiv1.SuggestionState {
	return apiv1.SuggestionState(apiv1.SuggestionState_value[s.String()])
}

func suggestionStateToKatana(s apiv1.SuggestionState) katanav1.SuggestionState {
	return katanav1.SuggestionState(katanav1.SuggestionState_value[s.String()])
}

func profileSuggestionToAPI(s *katanav1.Suggestion) *apiv1.ProfileSuggestion {
	evidence := make([]*apiv1.SuggestionEvidence, 0, len(s.GetEvidence()))
	for _, e := range s.GetEvidence() {
		evidence = append(evidence, &apiv1.SuggestionEvidence{Label: e.GetLabel(), Url: e.GetUrl()})
	}
	return &apiv1.ProfileSuggestion{
		Id: s.GetId(), Target: suggestionTargetToAPI(s.GetTarget()), Section: s.GetSection(), Before: s.GetBefore(),
		After: s.GetAfter(), Reason: s.GetReason(), Evidence: evidence, State: suggestionStateToAPI(s.GetState()),
		CreatedAt: s.GetCreatedAt(), DecidedAt: s.GetDecidedAt(),
	}
}

// SyncGitHub reads GitHub now.
func (s *ProfileServer) SyncGitHub(
	ctx context.Context, _ *connect.Request[apiv1.SyncGitHubRequest],
) (*connect.Response[apiv1.SyncGitHubResponse], error) {
	resp, err := s.katana.SyncGitHub(ctx, &katanav1.SyncGitHubRequest{})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.SyncGitHubResponse{Changed: resp.GetChanged()}), nil
}

// ListProfileSuggestions returns one page of suggestions, newest first.
func (s *ProfileServer) ListProfileSuggestions(
	ctx context.Context, req *connect.Request[apiv1.ListProfileSuggestionsRequest],
) (*connect.Response[apiv1.ListProfileSuggestionsResponse], error) {
	in := req.Msg
	resp, err := s.katana.ListSuggestions(ctx, &katanav1.ListSuggestionsRequest{
		State: suggestionStateToKatana(in.GetState()), Target: suggestionTargetToKatana(in.GetTarget()),
		PageSize: in.GetPageSize(), PageToken: in.GetPageToken(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	out := make([]*apiv1.ProfileSuggestion, 0, len(resp.GetSuggestions()))
	for _, sug := range resp.GetSuggestions() {
		out = append(out, profileSuggestionToAPI(sug))
	}
	return connect.NewResponse(&apiv1.ListProfileSuggestionsResponse{Suggestions: out, NextPageToken: resp.GetNextPageToken()}), nil
}

// AcceptProfileSuggestion records that the owner took a suggestion.
func (s *ProfileServer) AcceptProfileSuggestion(
	ctx context.Context, req *connect.Request[apiv1.AcceptProfileSuggestionRequest],
) (*connect.Response[apiv1.AcceptProfileSuggestionResponse], error) {
	resp, err := s.katana.AcceptSuggestion(ctx, &katanav1.AcceptSuggestionRequest{Id: req.Msg.GetId()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.AcceptProfileSuggestionResponse{Suggestion: profileSuggestionToAPI(resp.GetSuggestion())}), nil
}

// DismissProfileSuggestion records that the owner turned a suggestion down.
func (s *ProfileServer) DismissProfileSuggestion(
	ctx context.Context, req *connect.Request[apiv1.DismissProfileSuggestionRequest],
) (*connect.Response[apiv1.DismissProfileSuggestionResponse], error) {
	resp, err := s.katana.DismissSuggestion(ctx, &katanav1.DismissSuggestionRequest{Id: req.Msg.GetId()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.DismissProfileSuggestionResponse{Suggestion: profileSuggestionToAPI(resp.GetSuggestion())}), nil
}
