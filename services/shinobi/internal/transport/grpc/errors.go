package grpc

import (
	"errors"
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
)

// errorDomain is the ErrorInfo.domain of every error shinobi returns.
const errorDomain = "shinobi.shogun"

// Stable ErrorInfo.reason values.
const (
	reasonOwnerRequired   = "OWNER_REQUIRED"
	reasonInvalidArgument = "INVALID_ARGUMENT"
	reasonInvalidID       = "INVALID_ID"
	reasonInvalidToken    = "INVALID_PAGE_TOKEN"
	reasonSourceNotFound  = "SOURCE_NOT_FOUND"
	reasonKindFixed       = "SOURCE_KIND_FIXED"
	reasonTooManySources  = "TOO_MANY_SOURCES"
	reasonInternal        = "INTERNAL"

	// runReasonPrefix starts the reason of a source that could not be read; the
	// failure code follows in upper case, for example SOURCE_ROBOTS_DISALLOWED.
	runReasonPrefix = "SOURCE_"
)

// badRequest is a request the handler itself refuses before any use case runs.
type badRequest struct {
	reason string
	msg    string
}

func (e *badRequest) Error() string { return e.reason + ": " + e.msg }

// toStatus maps a use case error to a gRPC status carrying an ErrorInfo
// reason. Unexpected errors become Internal without their text; the server's
// log interceptor records the original.
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var (
		bad *badRequest
		run *app.RunError
	)
	switch {
	case errors.As(err, &bad):
		return withReason(codes.InvalidArgument, bad.reason, bad.msg)
	case errors.As(err, &run):
		return withReason(codes.FailedPrecondition, runReasonPrefix+strings.ToUpper(run.Code), "the source could not be read: "+run.Code)
	case errors.Is(err, app.ErrNoOwner):
		return withReason(codes.PermissionDenied, reasonOwnerRequired, "call has no valid owner")
	case errors.Is(err, domain.ErrInvalid):
		return withReason(codes.InvalidArgument, reasonInvalidArgument, err.Error())
	case errors.Is(err, app.ErrSourceNotFound):
		return withReason(codes.NotFound, reasonSourceNotFound, "source not found")
	case errors.Is(err, app.ErrKindFixed):
		return withReason(codes.FailedPrecondition, reasonKindFixed, "a source's kind cannot change; add a new source instead")
	case errors.Is(err, app.ErrTooManySources):
		return withReason(codes.ResourceExhausted, reasonTooManySources, "too many sources; remove one first")
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
