package grpc

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/services/dojo/internal/app"
	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store"
)

// errorDomain is the ErrorInfo.domain of every error dojo returns.
const errorDomain = "dojo.shogun"

// Stable ErrorInfo.reason values.
const (
	reasonOwnerRequired     = "OWNER_REQUIRED"
	reasonInvalidArgument   = "INVALID_ARGUMENT"
	reasonInvalidID         = "INVALID_ID"
	reasonInvalidMask       = "INVALID_UPDATE_MASK"
	reasonInvalidDate       = "INVALID_DATE"
	reasonInvalidToken      = "INVALID_PAGE_TOKEN"
	reasonItemNotFound      = "ITEM_NOT_FOUND"
	reasonActivityNotFound  = "ACTIVITY_NOT_FOUND"
	reasonVersionConflict   = "VERSION_CONFLICT"
	reasonDraftsUnavailable = "DRAFTS_UNAVAILABLE"
	reasonInternal          = "INTERNAL"
)

// badRequest is a request the handler itself refuses before any use case runs.
type badRequest struct {
	reason string
	msg    string
}

func (e *badRequest) Error() string { return e.reason + ": " + e.msg }

// toStatus maps a use case error to a gRPC status carrying an ErrorInfo
// reason. Unexpected errors become Internal without text; the server's log
// interceptor records the original.
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var (
		bad        *badRequest
		transition *domain.TransitionError
	)
	switch {
	case errors.As(err, &bad):
		return withReason(codes.InvalidArgument, bad.reason, bad.msg)
	case errors.As(err, &transition):
		return withReason(codes.FailedPrecondition, transition.Reason, "item cannot move from "+transition.From+" to "+transition.To)
	case errors.Is(err, app.ErrNoOwner):
		return withReason(codes.PermissionDenied, reasonOwnerRequired, "call has no valid owner")
	case errors.Is(err, domain.ErrInvalid):
		return withReason(codes.InvalidArgument, reasonInvalidArgument, err.Error())
	case errors.Is(err, app.ErrItemNotFound):
		return withReason(codes.NotFound, reasonItemNotFound, "item not found")
	case errors.Is(err, app.ErrActivityNotFound):
		return withReason(codes.NotFound, reasonActivityNotFound, "activity not found")
	case errors.Is(err, store.ErrVersionConflict):
		return withReason(codes.Aborted, reasonVersionConflict, "item changed elsewhere; read the latest and retry")
	case errors.Is(err, store.ErrInvalidPageToken):
		return withReason(codes.InvalidArgument, reasonInvalidToken, "page token is not valid")
	case errors.Is(err, app.ErrDraftsUnavailable):
		return withReason(codes.Unavailable, reasonDraftsUnavailable, "drafts are unavailable; try again later")
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
