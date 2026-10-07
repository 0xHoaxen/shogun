package grpc

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
)

// errorDomain is the ErrorInfo.domain of every error taiko returns.
const errorDomain = "taiko.shogun"

// Reasons for errors that have no code of their own in app.
const (
	reasonInvalidToken    = "INVALID_PAGE_TOKEN"
	reasonOwnerRequired   = "OWNER_REQUIRED"
	reasonStreamReset     = "STREAM_RESET"
	reasonInternal        = "INTERNAL"
	reasonVersionConflict = "VERSION_CONFLICT"
)

// toStatus maps a use case error to a gRPC status carrying an ErrorInfo reason.
// Unexpected errors become Internal without their text; the server's log
// interceptor records the original.
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var ia *app.InvalidArgumentError
	switch {
	case errors.As(err, &ia):
		return withReason(codes.InvalidArgument, ia.Reason, ia.Msg)
	case errors.Is(err, app.ErrNoOwner):
		return withReason(codes.PermissionDenied, reasonOwnerRequired, "call has no valid owner")
	case errors.Is(err, app.ErrStreamReset):
		return withReason(codes.Unavailable, reasonStreamReset, "stream was reset; reconnect with the last seen id")
	case errors.Is(err, store.ErrVersionConflict):
		return withReason(codes.Aborted, reasonVersionConflict, "settings changed elsewhere; read the latest and retry")
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
