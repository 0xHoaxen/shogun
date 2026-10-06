package grpc

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
)

// errorDomain is the ErrorInfo.domain of every error tsubame returns.
const errorDomain = "tsubame.shogun"

// Stable ErrorInfo.reason values.
const (
	reasonInvalidProvider = "INVALID_PROVIDER"
	reasonInvalidState    = "INVALID_STATE"
	reasonInvalidCode     = "INVALID_CODE"
	reasonOwnerRequired   = "OWNER_REQUIRED"
	reasonInternal        = "INTERNAL"
)

// toStatus maps a use case error to a gRPC status carrying an ErrorInfo reason.
// Unexpected errors become Internal without their text; the server's log
// interceptor records the original.
func toStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, app.ErrInvalidProvider):
		return withReason(codes.InvalidArgument, reasonInvalidProvider, "provider is missing or not supported")
	case errors.Is(err, app.ErrInvalidState):
		return withReason(codes.InvalidArgument, reasonInvalidState, "state is invalid or has expired; start again")
	case errors.Is(err, app.ErrInvalidCode):
		return withReason(codes.InvalidArgument, reasonInvalidCode, "the provider refused the code; start again")
	case errors.Is(err, app.ErrNoOwner):
		return withReason(codes.PermissionDenied, reasonOwnerRequired, "call has no valid owner")
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
