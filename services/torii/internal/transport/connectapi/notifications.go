package connectapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
)

// maxStreamAge is how long a stream stays open. The session is checked when a
// stream opens, so ending it now and then makes the browser reconnect, and
// reconnecting checks the session again.
const maxStreamAge = 10 * time.Minute

const reasonStreamReset = "STREAM_RESET"

// NotificationsBackend is the part of taiko's client NotificationsServer uses;
// taikov1.TaikoServiceClient implements it.
type NotificationsBackend interface {
	List(ctx context.Context, in *taikov1.ListRequest, opts ...grpc.CallOption) (*taikov1.ListResponse, error)
	MarkRead(ctx context.Context, in *taikov1.MarkReadRequest, opts ...grpc.CallOption) (*taikov1.MarkReadResponse, error)
	MarkAllRead(ctx context.Context, in *taikov1.MarkAllReadRequest, opts ...grpc.CallOption) (*taikov1.MarkAllReadResponse, error)
	Subscribe(ctx context.Context, in *taikov1.SubscribeRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[taikov1.SubscribeResponse], error)
	GetChannelSettings(ctx context.Context, in *taikov1.GetChannelSettingsRequest, opts ...grpc.CallOption) (*taikov1.GetChannelSettingsResponse, error)
	SaveChannelSettings(ctx context.Context, in *taikov1.SaveChannelSettingsRequest, opts ...grpc.CallOption) (*taikov1.SaveChannelSettingsResponse, error)
}

// NotificationsServer implements shogun.api.v1.NotificationsService on taiko.
type NotificationsServer struct {
	taiko  NotificationsBackend
	log    *slog.Logger
	maxAge time.Duration
}

var _ apiv1connect.NotificationsServiceHandler = (*NotificationsServer)(nil)

// NotificationsOption changes how a NotificationsServer behaves.
type NotificationsOption func(*NotificationsServer)

// WithMaxStreamAge sets how long a stream stays open before it ends cleanly.
func WithMaxStreamAge(d time.Duration) NotificationsOption {
	return func(s *NotificationsServer) { s.maxAge = d }
}

// NewNotificationsServer returns a NotificationsServer that calls backend.
func NewNotificationsServer(backend NotificationsBackend, log *slog.Logger, opts ...NotificationsOption) *NotificationsServer {
	s := &NotificationsServer{taiko: backend, log: log, maxAge: maxStreamAge}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ListNotifications returns one page of notifications and the unread count.
func (s *NotificationsServer) ListNotifications(
	ctx context.Context, req *connect.Request[apiv1.ListNotificationsRequest],
) (*connect.Response[apiv1.ListNotificationsResponse], error) {
	in := req.Msg
	resp, err := s.taiko.List(ctx, &taikov1.ListRequest{
		UnreadOnly: in.GetUnreadOnly(), PageSize: in.GetPageSize(), PageToken: in.GetPageToken(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	notifications := make([]*apiv1.Notification, 0, len(resp.GetNotifications()))
	for _, n := range resp.GetNotifications() {
		notifications = append(notifications, notificationToAPI(n))
	}
	return connect.NewResponse(&apiv1.ListNotificationsResponse{
		Notifications: notifications, NextPageToken: resp.GetNextPageToken(), UnreadCount: resp.GetUnreadCount(),
	}), nil
}

// MarkNotificationsRead marks the listed notifications read.
func (s *NotificationsServer) MarkNotificationsRead(
	ctx context.Context, req *connect.Request[apiv1.MarkNotificationsReadRequest],
) (*connect.Response[apiv1.MarkNotificationsReadResponse], error) {
	if _, err := s.taiko.MarkRead(ctx, &taikov1.MarkReadRequest{Ids: req.Msg.GetIds()}); err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.MarkNotificationsReadResponse{}), nil
}

// MarkAllNotificationsRead marks every unread notification read.
func (s *NotificationsServer) MarkAllNotificationsRead(
	ctx context.Context, _ *connect.Request[apiv1.MarkAllNotificationsReadRequest],
) (*connect.Response[apiv1.MarkAllNotificationsReadResponse], error) {
	if _, err := s.taiko.MarkAllRead(ctx, &taikov1.MarkAllReadRequest{}); err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.MarkAllNotificationsReadResponse{}), nil
}

// Stream bridges taiko's Subscribe to the browser. It ends cleanly when the
// browser leaves or the stream reaches its maximum age, and with Unavailable
// when taiko reset the stream; in every case the browser reconnects from the
// last id it saw.
func (s *NotificationsServer) Stream(
	ctx context.Context, req *connect.Request[apiv1.StreamRequest], stream *connect.ServerStream[apiv1.StreamResponse],
) error {
	ctx, cancel := context.WithTimeout(ctx, s.maxAge)
	defer cancel()
	sub, err := s.taiko.Subscribe(ctx, &taikov1.SubscribeRequest{AfterId: req.Msg.GetLastSeenId()})
	if err != nil {
		return fromGRPC(ctx, s.log, err)
	}
	for {
		res, err := sub.Recv()
		switch {
		case err == nil:
			if sendErr := stream.Send(&apiv1.StreamResponse{Notification: notificationToAPI(res.GetNotification())}); sendErr != nil {
				return sendErr
			}
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, io.EOF):
			return newError(connect.CodeUnavailable, reasonStreamReset, "the stream ended; reconnect with the last seen id")
		default:
			return fromGRPC(ctx, s.log, err)
		}
	}
}

// GetNotificationSettings returns how the owner is reached.
func (s *NotificationsServer) GetNotificationSettings(
	ctx context.Context, _ *connect.Request[apiv1.GetNotificationSettingsRequest],
) (*connect.Response[apiv1.GetNotificationSettingsResponse], error) {
	resp, err := s.taiko.GetChannelSettings(ctx, &taikov1.GetChannelSettingsRequest{})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.GetNotificationSettingsResponse{Settings: settingsToAPI(resp.GetSettings())}), nil
}

// SaveNotificationSettings replaces how the owner is reached.
func (s *NotificationsServer) SaveNotificationSettings(
	ctx context.Context, req *connect.Request[apiv1.SaveNotificationSettingsRequest],
) (*connect.Response[apiv1.SaveNotificationSettingsResponse], error) {
	resp, err := s.taiko.SaveChannelSettings(ctx, &taikov1.SaveChannelSettingsRequest{
		Settings: settingsFromAPI(req.Msg.GetSettings()),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.SaveNotificationSettingsResponse{Settings: settingsToAPI(resp.GetSettings())}), nil
}
