// Package grpc holds the tsubame gRPC handlers. They convert requests to use
// case inputs, call internal/app, and map its errors to gRPC status codes.
package grpc

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

// Server implements tsubame.v1.TsubameService. RPCs without a handler yet
// answer Unimplemented.
type Server struct {
	tsubamev1.UnimplementedTsubameServiceServer
	connector *app.Connector
	sender    *app.Sender
}

// New returns a Server that connects accounts through connector and sends
// approved drafts through sender.
func New(connector *app.Connector, sender *app.Sender) *Server {
	return &Server{connector: connector, sender: sender}
}

// ConnectAccount implements tsubame.v1.TsubameService.
func (s *Server) ConnectAccount(ctx context.Context, req *tsubamev1.ConnectAccountRequest) (*tsubamev1.ConnectAccountResponse, error) {
	provider := ""
	if req.GetProvider() == tsubamev1.MailProvider_MAIL_PROVIDER_GMAIL {
		provider = app.ProviderGmail
	}
	url, err := s.connector.Begin(ctx, provider)
	if err != nil {
		return nil, toStatus(err)
	}
	return &tsubamev1.ConnectAccountResponse{AuthUrl: url}, nil
}

// CompleteConnect implements tsubame.v1.TsubameService.
func (s *Server) CompleteConnect(ctx context.Context, req *tsubamev1.CompleteConnectRequest) (*tsubamev1.CompleteConnectResponse, error) {
	acc, err := s.connector.Complete(ctx, req.GetCode(), req.GetState())
	if err != nil {
		return nil, toStatus(err)
	}
	return &tsubamev1.CompleteConnectResponse{Account: accountToProto(acc)}, nil
}

// accountToProto converts an account. The sealed token is left behind.
func accountToProto(a db.Account) *tsubamev1.Account {
	out := &tsubamev1.Account{
		Id: a.ID.String(), Address: a.Address, CreatedAt: timestamppb.New(a.CreatedAt),
		Provider: tsubamev1.MailProvider_MAIL_PROVIDER_UNSPECIFIED,
	}
	if a.Provider == app.ProviderGmail {
		out.Provider = tsubamev1.MailProvider_MAIL_PROVIDER_GMAIL
	}
	switch a.Status {
	case "active":
		out.Status = tsubamev1.AccountStatus_ACCOUNT_STATUS_ACTIVE
	case "reauth_required":
		out.Status = tsubamev1.AccountStatus_ACCOUNT_STATUS_REAUTH_REQUIRED
	case "disabled":
		out.Status = tsubamev1.AccountStatus_ACCOUNT_STATUS_DISABLED
	}
	if a.LastSyncedAt != nil {
		out.LastSyncedAt = timestamppb.New(*a.LastSyncedAt)
	}
	return out
}

// Send implements tsubame.v1.TsubameService.
func (s *Server) Send(ctx context.Context, req *tsubamev1.SendRequest) (*tsubamev1.SendResponse, error) {
	id, err := s.sender.Send(ctx, app.SendInput{
		Hanko: req.GetHanko(), DraftID: req.GetDraftId(), Version: req.GetVersion(), To: req.GetTo(),
		Subject: req.GetSubject(), Body: req.GetBody(), ContactID: req.GetContactId(), JobID: req.GetJobId(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &tsubamev1.SendResponse{ProviderMessageId: id}, nil
}
