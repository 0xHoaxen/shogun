package grpc

import (
	"errors"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/services/soroban/internal/app"
	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
)

// errorDomain is the ErrorInfo.domain of every error soroban returns.
const errorDomain = "soroban.shogun"

// Reasons for errors that have no code of their own in app or domain.
const (
	reasonOwnerRequired      = "OWNER_REQUIRED"
	reasonNotFound           = "RESOURCE_NOT_FOUND"
	reasonReservationNotOpen = "RESERVATION_NOT_OPEN"
	reasonInternal           = "INTERNAL"
)

// toStatus maps a use case error to a gRPC status carrying an ErrorInfo
// reason. Unexpected errors become Internal without their text; the server's
// log interceptor records the original.
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var exhausted *domain.BudgetExhaustedError
	var ia *app.InvalidArgumentError
	switch {
	case errors.As(err, &exhausted):
		return withReason(codes.ResourceExhausted, domain.ReasonBudgetExhausted, exhausted.Error(), map[string]string{
			"budget_id": exhausted.BudgetID.String(),
			"resets_at": exhausted.ResetsAt.UTC().Format(time.RFC3339),
		})
	case errors.As(err, &ia):
		return withReason(codes.InvalidArgument, ia.Reason, ia.Msg, nil)
	case errors.Is(err, app.ErrNoOwner):
		return withReason(codes.PermissionDenied, reasonOwnerRequired, "call has no valid owner", nil)
	case errors.Is(err, store.ErrNotFound):
		return withReason(codes.NotFound, reasonNotFound, "not found", nil)
	case errors.Is(err, app.ErrReservationNotOpen):
		return withReason(codes.FailedPrecondition, reasonReservationNotOpen, "reservation is not open", nil)
	default:
		return withReason(codes.Internal, reasonInternal, "internal error", nil)
	}
}

func withReason(code codes.Code, reason, msg string, metadata map[string]string) error {
	st := status.New(code, msg)
	detailed, err := st.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: errorDomain, Metadata: metadata})
	if err != nil {
		return st.Err()
	}
	return detailed.Err()
}
