// Package tsubame sends approved drafts through the tsubame service for fude.
package tsubame

import (
	"context"
	"fmt"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
)

// Reasons tsubame gives its refusals.
const (
	reasonApprovalUsed  = "APPROVAL_ALREADY_USED"
	reasonAlreadySent   = "ALREADY_SENT"
	reasonNotApproved   = "NOT_APPROVED"
	reasonNoMailAccount = "NO_MAIL_ACCOUNT"
	reasonSendFailed    = "SEND_FAILED"
)

// Mailer calls tsubame.Send. The context must carry the owner's identity, which
// the client signs onto the call. It implements app.Mailer.
type Mailer struct {
	client tsubamev1.TsubameServiceClient
}

// New returns a Mailer that calls tsubame through client.
func New(client tsubamev1.TsubameServiceClient) *Mailer { return &Mailer{client: client} }

// Send implements app.Mailer. The token is passed on and never logged or put in
// an error.
func (m *Mailer) Send(ctx context.Context, req app.SendRequest) error {
	_, err := m.client.Send(ctx, &tsubamev1.SendRequest{
		Hanko: req.Token, DraftId: req.DraftID.String(), Version: req.Version, To: req.To,
		Subject: req.Subject, Body: req.Body, ContactId: req.ContactID, JobId: req.JobID,
	})
	return classify(err)
}

// classify maps what tsubame said to how fude must treat the send. Anything it
// does not recognise is returned as an unknown outcome, never as "not sent".
func classify(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("tsubame send: %w", err)
	}
	reason := reasonOf(st)
	switch {
	case st.Code() == codes.PermissionDenied && (reason == reasonApprovalUsed || reason == reasonAlreadySent):
		return app.ErrMailInFlight
	case st.Code() == codes.PermissionDenied && reason == reasonNotApproved,
		st.Code() == codes.InvalidArgument,
		st.Code() == codes.FailedPrecondition && reason == reasonNoMailAccount:
		return app.ErrMailRefused
	case st.Code() == codes.FailedPrecondition && reason == reasonSendFailed:
		return app.ErrMailFailed
	case st.Code() == codes.Unavailable:
		return app.ErrMailUnreachable
	default:
		return fmt.Errorf("tsubame send answered %s %s", st.Code(), reason)
	}
}

func reasonOf(st *status.Status) string {
	for _, d := range st.Details() {
		var info *errdetails.ErrorInfo
		if info, _ = d.(*errdetails.ErrorInfo); info != nil {
			return info.GetReason()
		}
	}
	return ""
}
