package grpc

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
)

// errorDomain is the ErrorInfo.domain of every error katana returns.
const errorDomain = "katana.shogun"

// Stable ErrorInfo.reason values.
const (
	reasonOwnerRequired      = "OWNER_REQUIRED"
	reasonGitHubNotSet       = "GITHUB_NOT_CONFIGURED"
	reasonGitHubNotConnected = "GITHUB_NOT_CONNECTED"
	reasonGitHubUnauthorized = "GITHUB_UNAUTHORIZED"
	reasonGitHubRateLimited  = "GITHUB_RATE_LIMITED"
	reasonGitHubUnavailable  = "GITHUB_UNAVAILABLE"
	reasonGitHubStorageOff   = "GITHUB_STORAGE_OFF"
	reasonInvalidState       = "INVALID_STATE"
	reasonInvalidCode        = "INVALID_CODE"
	reasonInternal           = "INTERNAL"
	reasonInvalidID          = "INVALID_ID"
	reasonInvalidToken       = "INVALID_PAGE_TOKEN"
	reasonNotFound           = "SUGGESTION_NOT_FOUND"
)

// toStatus maps a use case error to a gRPC status carrying an ErrorInfo
// reason. Unexpected errors become Internal without their text; the server's
// log interceptor records the original.
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var (
		limit    *app.RateLimitError
		decision *domain.DecisionError
		bad      *badRequest
	)
	switch {
	case errors.As(err, &bad):
		return withReason(codes.InvalidArgument, bad.reason, bad.msg)
	case errors.As(err, &decision):
		return withReason(codes.FailedPrecondition, decision.Reason, "the suggestion was already accepted or dismissed")
	case errors.Is(err, app.ErrSuggestionNotFound):
		return withReason(codes.NotFound, reasonNotFound, "suggestion not found")
	case errors.Is(err, store.ErrInvalidPageToken):
		return withReason(codes.InvalidArgument, reasonInvalidToken, "page token is not valid")
	case errors.Is(err, app.ErrNoOwner):
		return withReason(codes.PermissionDenied, reasonOwnerRequired, "call has no valid owner")
	case errors.Is(err, app.ErrInvalidState):
		return withReason(codes.InvalidArgument, reasonInvalidState, "state is invalid or has expired; start again")
	case errors.Is(err, app.ErrInvalidCode):
		return withReason(codes.InvalidArgument, reasonInvalidCode, "GitHub refused the code; start again")
	case errors.Is(err, app.ErrGitHubStorageOff):
		return withReason(codes.FailedPrecondition, reasonGitHubStorageOff, "this server cannot store GitHub connections")
	case errors.Is(err, app.ErrGitHubNotConfigured):
		return withReason(codes.FailedPrecondition, reasonGitHubNotSet, "connecting GitHub is not set up on this server")
	case errors.Is(err, app.ErrGitHubNotConnected):
		return withReason(codes.FailedPrecondition, reasonGitHubNotConnected, "connect your GitHub account first")
	case errors.Is(err, app.ErrGitHubUnauthorized):
		return withReason(codes.FailedPrecondition, reasonGitHubUnauthorized, "GitHub refused the token")
	case errors.As(err, &limit):
		return withReason(codes.ResourceExhausted, reasonGitHubRateLimited, "GitHub's rate limit is used up; try again later")
	case errors.Is(err, app.ErrGitHubUnavailable):
		return withReason(codes.Unavailable, reasonGitHubUnavailable, "GitHub is unavailable; try again later")
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

// badRequest is a request the handler itself refuses before any use case runs.
type badRequest struct {
	reason string
	msg    string
}

func (e *badRequest) Error() string { return e.reason + ": " + e.msg }
