package grpc

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/services/katana/internal/app"
)

// errorDomain is the ErrorInfo.domain of every error katana returns.
const errorDomain = "katana.shogun"

// Stable ErrorInfo.reason values.
const (
	reasonOwnerRequired      = "OWNER_REQUIRED"
	reasonGitHubNotSet       = "GITHUB_NOT_CONFIGURED"
	reasonGitHubUnauthorized = "GITHUB_UNAUTHORIZED"
	reasonGitHubRateLimited  = "GITHUB_RATE_LIMITED"
	reasonGitHubUnavailable  = "GITHUB_UNAVAILABLE"
	reasonInternal           = "INTERNAL"
)

// toStatus maps a use case error to a gRPC status carrying an ErrorInfo
// reason. Unexpected errors become Internal without their text; the server's
// log interceptor records the original.
func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var limit *app.RateLimitError
	switch {
	case errors.Is(err, app.ErrNoOwner):
		return withReason(codes.PermissionDenied, reasonOwnerRequired, "call has no valid owner")
	case errors.Is(err, app.ErrGitHubNotConfigured):
		return withReason(codes.FailedPrecondition, reasonGitHubNotSet, "no GitHub user or token is configured")
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
