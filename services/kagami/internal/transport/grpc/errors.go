package grpc

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
)

// errorDomain is the ErrorInfo.domain of every error kagami returns.
const errorDomain = "kagami.shogun"

// Reasons for errors that have no code of their own in app or domain.
const (
	reasonNotFound         = "RESOURCE_NOT_FOUND"
	reasonVersionConflict  = "VERSION_CONFLICT"
	reasonAlreadyExists    = "ALREADY_EXISTS"
	reasonContactDuplicate = "CONTACT_DUPLICATE"
	reasonInvalidToken     = "INVALID_PAGE_TOKEN"
	reasonOwnerRequired    = "OWNER_REQUIRED"
	reasonInternal         = "INTERNAL"
)

// toStatus maps a use case error to a gRPC status carrying an ErrorInfo reason.
// Unexpected errors become Internal without their text, which may hold row
// data; the server's log interceptor records the original.
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var te *domain.TransitionError
	var ia *app.InvalidArgumentError
	var dup *app.DuplicateContactError
	switch {
	case errors.As(err, &te):
		return withReason(codes.FailedPrecondition, te.Reason, te.Error())
	case errors.As(err, &dup):
		return withReason(codes.AlreadyExists, reasonContactDuplicate, dup.Error())
	case errors.As(err, &ia):
		return withReason(codes.InvalidArgument, ia.Reason, ia.Msg)
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
