package grpc

import (
	"errors"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
)

// errorDomain is the ErrorInfo.domain of every error fude returns.
const errorDomain = "fude.shogun"

// Reasons for errors that have no code of their own in app or domain.
const (
	reasonNotFound        = "RESOURCE_NOT_FOUND"
	reasonVersionConflict = "VERSION_CONFLICT"
	reasonAlreadyExists   = "ALREADY_EXISTS"
	reasonInvalidToken    = "INVALID_PAGE_TOKEN"
	reasonOwnerRequired   = "OWNER_REQUIRED"
	reasonSendRefused     = "SEND_REFUSED"
	reasonSendFailed      = "SEND_FAILED"
	reasonSendUnavailable = "SEND_UNAVAILABLE"
	reasonSendUnknown     = "SEND_STATUS_UNKNOWN"
	reasonInternal        = "INTERNAL"

	reasonNoProvider          = "AI_PROVIDER_NOT_CONFIGURED"
	reasonBudgetExhausted     = "BUDGET_EXHAUSTED"
	reasonMeteringUnavailable = "METERING_UNAVAILABLE"
)

// toStatus maps a use case error to a gRPC status carrying an ErrorInfo reason.
// Unexpected errors become Internal without their text, which may hold draft
// content; the server's log interceptor records the original.
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var te *domain.TransitionError
	var ia *app.InvalidArgumentError
	var budget *llm.BudgetError
	switch {
	case errors.Is(err, llm.ErrProviderNotConfigured):
		return withReason(codes.FailedPrecondition, reasonNoProvider, "no AI provider is set; add one in Settings")
	case errors.As(err, &budget):
		return budgetStatus(budget)
	case errors.Is(err, llm.ErrMeteringUnavailable):
		return withReason(codes.Unavailable, reasonMeteringUnavailable, "cost metering is unavailable; try again shortly")
	case errors.As(err, &te):
		return withReason(codes.FailedPrecondition, te.Reason, te.Error())
	case errors.As(err, &ia):
		return withReason(codes.InvalidArgument, ia.Reason, ia.Msg)
	case errors.Is(err, app.ErrSendRefused):
		return withReason(codes.FailedPrecondition, reasonSendRefused, "the mail could not be sent, for example because no mail account is connected; the draft is back in the queue")
	case errors.Is(err, app.ErrSendFailed):
		return withReason(codes.FailedPrecondition, reasonSendFailed, "the mail provider did not send the mail; the draft is back in the queue")
	case errors.Is(err, app.ErrSendUnavailable):
		return withReason(codes.Unavailable, reasonSendUnavailable, "the mail service is unavailable; the draft is back in the queue")
	case errors.Is(err, app.ErrSendUnknown):
		return withReason(codes.Unavailable, reasonSendUnknown, "could not confirm whether the mail was sent; check again shortly")
	case errors.Is(err, app.ErrNoOwner):
		return withReason(codes.PermissionDenied, reasonOwnerRequired, "call has no valid owner")
	case errors.Is(err, store.ErrNotFound):
		return withReason(codes.NotFound, reasonNotFound, "not found")
	case errors.Is(err, store.ErrVersionConflict):
		return withReason(codes.Aborted, reasonVersionConflict, "version is stale; read the latest and retry")
	case errors.Is(err, store.ErrDuplicate):
		return withReason(codes.AlreadyExists, reasonAlreadyExists, "already exists")
	case errors.Is(err, store.ErrInvalidPageToken):
		return withReason(codes.InvalidArgument, reasonInvalidToken, "page token is not valid")
	default:
		return withReason(codes.Internal, reasonInternal, "internal error")
	}
}

func withReason(code codes.Code, reason, msg string) error {
	st := status.New(code, msg)
	detailed, err := st.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: errorDomain})
	if err != nil {
		return st.Err()
	}
	return detailed.Err()
}

// budgetStatus is the refusal of a spent budget or token cap, with when it ends.
func budgetStatus(b *llm.BudgetError) error {
	st := status.New(codes.ResourceExhausted, "a budget or token cap is used up")
	meta := map[string]string{}
	if b.BudgetID != "" {
		meta["budget_id"] = b.BudgetID
	}
	if !b.ResetsAt.IsZero() {
		meta["resets_at"] = b.ResetsAt.UTC().Format(time.RFC3339)
	}
	detailed, err := st.WithDetails(&errdetails.ErrorInfo{Reason: reasonBudgetExhausted, Domain: errorDomain, Metadata: meta})
	if err != nil {
		return st.Err()
	}
	return detailed.Err()
}
