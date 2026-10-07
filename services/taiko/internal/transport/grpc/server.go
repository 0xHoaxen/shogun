package grpc

import (
	"context"

	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

// Server implements taiko.v1.TaikoService.
type Server struct {
	taikov1.UnimplementedTaikoServiceServer
	svc *app.Service
}

// New returns a Server that runs its calls on svc.
func New(svc *app.Service) *Server {
	return &Server{svc: svc}
}

// List implements taiko.v1.TaikoService.
func (s *Server) List(ctx context.Context, req *taikov1.ListRequest) (*taikov1.ListResponse, error) {
	listing, err := s.svc.List(ctx, req.GetUnreadOnly(), store.Page{Size: req.GetPageSize(), Token: req.GetPageToken()})
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*taikov1.Notification, 0, len(listing.Notifications))
	for _, n := range listing.Notifications {
		out = append(out, notificationToProto(n))
	}
	return &taikov1.ListResponse{
		Notifications: out, NextPageToken: listing.NextPageToken, UnreadCount: listing.UnreadCount,
	}, nil
}

// MarkRead implements taiko.v1.TaikoService.
func (s *Server) MarkRead(ctx context.Context, req *taikov1.MarkReadRequest) (*taikov1.MarkReadResponse, error) {
	if err := s.svc.MarkRead(ctx, req.GetIds()); err != nil {
		return nil, toStatus(err)
	}
	return &taikov1.MarkReadResponse{}, nil
}

// MarkAllRead implements taiko.v1.TaikoService.
func (s *Server) MarkAllRead(ctx context.Context, _ *taikov1.MarkAllReadRequest) (*taikov1.MarkAllReadResponse, error) {
	if err := s.svc.MarkAllRead(ctx); err != nil {
		return nil, toStatus(err)
	}
	return &taikov1.MarkAllReadResponse{}, nil
}

// Subscribe implements taiko.v1.TaikoService.
func (s *Server) Subscribe(req *taikov1.SubscribeRequest, stream taikov1.TaikoService_SubscribeServer) error {
	err := s.svc.Subscribe(stream.Context(), req.GetAfterId(),
		// Headers tell the client the stream is registered.
		func() error { return stream.SendHeader(nil) },
		func(n db.Notification) error {
			return stream.Send(&taikov1.SubscribeResponse{Notification: notificationToProto(n)})
		})
	return toStatus(err)
}

// GetChannelSettings implements taiko.v1.TaikoService.
func (s *Server) GetChannelSettings(ctx context.Context, _ *taikov1.GetChannelSettingsRequest) (*taikov1.GetChannelSettingsResponse, error) {
	view, err := s.svc.ChannelSettings(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	return &taikov1.GetChannelSettingsResponse{Settings: settingsToProto(view)}, nil
}

// SaveChannelSettings implements taiko.v1.TaikoService.
func (s *Server) SaveChannelSettings(ctx context.Context, req *taikov1.SaveChannelSettingsRequest) (*taikov1.SaveChannelSettingsResponse, error) {
	in := req.GetSettings()
	view, err := s.svc.SaveChannelSettings(ctx, settingsFromProto(in), in.GetVersion())
	if err != nil {
		return nil, toStatus(err)
	}
	return &taikov1.SaveChannelSettingsResponse{Settings: settingsToProto(view)}, nil
}
