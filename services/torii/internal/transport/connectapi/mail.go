package connectapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/grpc"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
)

// MailBackend is the part of tsubame's client MailServer uses.
type MailBackend interface {
	ConnectAccount(ctx context.Context, in *tsubamev1.ConnectAccountRequest, opts ...grpc.CallOption) (*tsubamev1.ConnectAccountResponse, error)
	CompleteConnect(ctx context.Context, in *tsubamev1.CompleteConnectRequest, opts ...grpc.CallOption) (*tsubamev1.CompleteConnectResponse, error)
}

// MailServer implements shogun.api.v1.MailService on top of tsubame.
type MailServer struct {
	tsubame MailBackend
	log     *slog.Logger
}

var _ apiv1connect.MailServiceHandler = (*MailServer)(nil)

// NewMailServer returns a MailServer that calls tsubame through backend.
func NewMailServer(backend MailBackend, log *slog.Logger) *MailServer {
	return &MailServer{tsubame: backend, log: log}
}

// ConnectAccount starts connecting Gmail and returns where to send the owner.
func (s *MailServer) ConnectAccount(
	ctx context.Context, _ *connect.Request[apiv1.ConnectAccountRequest],
) (*connect.Response[apiv1.ConnectAccountResponse], error) {
	resp, err := s.tsubame.ConnectAccount(ctx, &tsubamev1.ConnectAccountRequest{Provider: tsubamev1.MailProvider_MAIL_PROVIDER_GMAIL})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.ConnectAccountResponse{AuthUrl: resp.GetAuthUrl()}), nil
}

// CompleteConnect finishes connecting with the code and state Google sent back.
// The code and state are never logged.
func (s *MailServer) CompleteConnect(
	ctx context.Context, req *connect.Request[apiv1.CompleteConnectRequest],
) (*connect.Response[apiv1.CompleteConnectResponse], error) {
	resp, err := s.tsubame.CompleteConnect(ctx, &tsubamev1.CompleteConnectRequest{Code: req.Msg.GetCode(), State: req.Msg.GetState()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.CompleteConnectResponse{Address: resp.GetAccount().GetAddress()}), nil
}
