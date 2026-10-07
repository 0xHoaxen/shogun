package app

import "github.com/0xHoaxen/shogun/services/katana/internal/domain"

// Errors from reading GitHub, re-exported so transport depends on app only.
var (
	ErrGitHubNotConfigured = domain.ErrNotConfigured
	ErrGitHubUnauthorized  = domain.ErrUnauthorized
	ErrGitHubUnavailable   = domain.ErrUnavailable

	errNotModified = domain.ErrNotModified
)

// RateLimitError means GitHub's rate limit is used up until ResetAt.
type RateLimitError = domain.RateLimitError
