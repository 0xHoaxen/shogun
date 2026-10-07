package connectapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/grpc"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	shinobiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/shinobi/v1"
)

// DiscoveryBackend is the part of shinobi's client DiscoveryServer uses.
type DiscoveryBackend interface {
	ListPostings(ctx context.Context, in *shinobiv1.ListPostingsRequest, opts ...grpc.CallOption) (*shinobiv1.ListPostingsResponse, error)
	SaveToTracker(ctx context.Context, in *shinobiv1.SaveToTrackerRequest, opts ...grpc.CallOption) (*shinobiv1.SaveToTrackerResponse, error)
	ListSources(ctx context.Context, in *shinobiv1.ListSourcesRequest, opts ...grpc.CallOption) (*shinobiv1.ListSourcesResponse, error)
	UpsertSource(ctx context.Context, in *shinobiv1.UpsertSourceRequest, opts ...grpc.CallOption) (*shinobiv1.UpsertSourceResponse, error)
	RunSource(ctx context.Context, in *shinobiv1.RunSourceRequest, opts ...grpc.CallOption) (*shinobiv1.RunSourceResponse, error)
	GetPreferences(ctx context.Context, in *shinobiv1.GetPreferencesRequest, opts ...grpc.CallOption) (*shinobiv1.GetPreferencesResponse, error)
	SetPreferences(ctx context.Context, in *shinobiv1.SetPreferencesRequest, opts ...grpc.CallOption) (*shinobiv1.SetPreferencesResponse, error)
}

// DiscoveryServer implements shogun.api.v1.DiscoveryService on top of shinobi.
type DiscoveryServer struct {
	shinobi DiscoveryBackend
	log     *slog.Logger
}

var _ apiv1connect.DiscoveryServiceHandler = (*DiscoveryServer)(nil)

// NewDiscoveryServer returns a DiscoveryServer that calls shinobi through backend.
func NewDiscoveryServer(backend DiscoveryBackend, log *slog.Logger) *DiscoveryServer {
	return &DiscoveryServer{shinobi: backend, log: log}
}

// ListDiscoveryPostings returns one page of postings, best score first.
func (s *DiscoveryServer) ListDiscoveryPostings(
	ctx context.Context, req *connect.Request[apiv1.ListDiscoveryPostingsRequest],
) (*connect.Response[apiv1.ListDiscoveryPostingsResponse], error) {
	in := req.Msg
	resp, err := s.shinobi.ListPostings(ctx, &shinobiv1.ListPostingsRequest{
		MinScore: in.GetMinScore(), SourceId: in.GetSourceId(), PageSize: in.GetPageSize(), PageToken: in.GetPageToken(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	out := make([]*apiv1.DiscoveryPosting, 0, len(resp.GetPostings()))
	for _, p := range resp.GetPostings() {
		out = append(out, discoveryPostingToAPI(p))
	}
	return connect.NewResponse(&apiv1.ListDiscoveryPostingsResponse{Postings: out, NextPageToken: resp.GetNextPageToken()}), nil
}

// SaveDiscoveryPosting adds a posting to the tracker.
func (s *DiscoveryServer) SaveDiscoveryPosting(
	ctx context.Context, req *connect.Request[apiv1.SaveDiscoveryPostingRequest],
) (*connect.Response[apiv1.SaveDiscoveryPostingResponse], error) {
	resp, err := s.shinobi.SaveToTracker(ctx, &shinobiv1.SaveToTrackerRequest{PostingId: req.Msg.GetPostingId()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.SaveDiscoveryPostingResponse{JobId: resp.GetJobId()}), nil
}

// ListDiscoverySources returns the owner's sources.
func (s *DiscoveryServer) ListDiscoverySources(
	ctx context.Context, _ *connect.Request[apiv1.ListDiscoverySourcesRequest],
) (*connect.Response[apiv1.ListDiscoverySourcesResponse], error) {
	resp, err := s.shinobi.ListSources(ctx, &shinobiv1.ListSourcesRequest{})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	out := make([]*apiv1.DiscoverySource, 0, len(resp.GetSources()))
	for _, src := range resp.GetSources() {
		out = append(out, discoverySourceToAPI(src))
	}
	return connect.NewResponse(&apiv1.ListDiscoverySourcesResponse{Sources: out}), nil
}

// SaveDiscoverySource creates or updates a source.
func (s *DiscoveryServer) SaveDiscoverySource(
	ctx context.Context, req *connect.Request[apiv1.SaveDiscoverySourceRequest],
) (*connect.Response[apiv1.SaveDiscoverySourceResponse], error) {
	resp, err := s.shinobi.UpsertSource(ctx, &shinobiv1.UpsertSourceRequest{Source: discoverySourceToShinobi(req.Msg.GetSource())})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.SaveDiscoverySourceResponse{Source: discoverySourceToAPI(resp.GetSource())}), nil
}

// RunDiscoverySource reads a source now.
func (s *DiscoveryServer) RunDiscoverySource(
	ctx context.Context, req *connect.Request[apiv1.RunDiscoverySourceRequest],
) (*connect.Response[apiv1.RunDiscoverySourceResponse], error) {
	resp, err := s.shinobi.RunSource(ctx, &shinobiv1.RunSourceRequest{Id: req.Msg.GetId()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.RunDiscoverySourceResponse{Fetched: resp.GetFetched(), Added: resp.GetAdded()}), nil
}

// GetDiscoveryPreferences returns what the owner is looking for.
func (s *DiscoveryServer) GetDiscoveryPreferences(
	ctx context.Context, _ *connect.Request[apiv1.GetDiscoveryPreferencesRequest],
) (*connect.Response[apiv1.GetDiscoveryPreferencesResponse], error) {
	resp, err := s.shinobi.GetPreferences(ctx, &shinobiv1.GetPreferencesRequest{})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.GetDiscoveryPreferencesResponse{Preferences: discoveryPreferencesToAPI(resp.GetPreferences())}), nil
}

// SetDiscoveryPreferences saves what the owner is looking for.
func (s *DiscoveryServer) SetDiscoveryPreferences(
	ctx context.Context, req *connect.Request[apiv1.SetDiscoveryPreferencesRequest],
) (*connect.Response[apiv1.SetDiscoveryPreferencesResponse], error) {
	resp, err := s.shinobi.SetPreferences(ctx, &shinobiv1.SetPreferencesRequest{
		Preferences: discoveryPreferencesToShinobi(req.Msg.GetPreferences()),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.SetDiscoveryPreferencesResponse{Preferences: discoveryPreferencesToAPI(resp.GetPreferences())}), nil
}
