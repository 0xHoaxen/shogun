package connectapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	reasonUnavailable = "UNAVAILABLE"
	reasonRejected    = "REJECTED"
)

// fromGRPC turns an error from a downstream service into a Connect error.
//
// Errors the owner can act on (a bad field, a stale version, an invalid status
// move) keep their code, message and stable ErrorInfo.reason. Everything else
// is logged here and replaced with a generic message, so internals never reach
// the browser. Connect and gRPC share their numeric status codes.
func fromGRPC(ctx context.Context, log *slog.Logger, err error) error {
	st, ok := status.FromError(err)
	if !ok {
		log.ErrorContext(ctx, "downstream call failed", slog.Any("error", err))
		return newError(connect.CodeInternal, reasonInternal, "something went wrong")
	}
	switch st.Code() { //nolint:exhaustive // every other code is treated as internal below
	case codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.ResourceExhausted:
		return newError(connect.Code(st.Code()), reasonOf(st), st.Message())
	case codes.Unavailable, codes.DeadlineExceeded:
		log.WarnContext(ctx, "downstream unavailable", slog.String("code", st.Code().String()))
		return newError(connect.Code(st.Code()), reasonUnavailable, "the service is busy, try again")
	default:
		// Includes Unauthenticated and PermissionDenied: torii signed the call, so
		// a refusal is a bug on our side, not something the owner can fix.
		log.ErrorContext(ctx, "downstream call failed",
			slog.String("code", st.Code().String()), slog.String("message", st.Message()))
		return newError(connect.CodeInternal, reasonInternal, "something went wrong")
	}
}

// reasonOf returns the ErrorInfo.reason a downstream service attached.
func reasonOf(st *status.Status) string {
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.GetReason() != "" {
			return info.GetReason()
		}
	}
	return reasonRejected
}
