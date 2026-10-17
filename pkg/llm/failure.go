package llm

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/0xHoaxen/shogun/pkg/netguard"
)

// FailureKind says, in a few words, why a call to a user's provider failed. It
// is what a service shows the user; the provider's own text is never passed on,
// since it can echo the credentials it was sent.
type FailureKind string

// The kinds of failure.
const (
	// FailureRejected means the provider refused the key (401 or 403).
	FailureRejected FailureKind = "rejected"
	// FailureRateLimited means the provider asked to slow down (429).
	FailureRateLimited FailureKind = "rate_limited"
	// FailureNotFound means the provider does not know the model or the address
	// path (404, or a request it could not accept).
	FailureNotFound FailureKind = "not_found"
	// FailureUnreachable means no answer came: refused, no route, or too slow.
	FailureUnreachable FailureKind = "unreachable"
	// FailureNotPublic means the address names this machine or a private
	// network, which is never called.
	FailureNotPublic FailureKind = "not_public"
	// FailureProviderError means the provider failed on its side (5xx).
	FailureProviderError FailureKind = "provider_error"
	// FailureOther is anything else.
	FailureOther FailureKind = "other"
)

// ClassifyFailure names why err, from a call to a provider, happened. It sees
// through any wrapping. A nil error has no kind.
func ClassifyFailure(err error) FailureKind {
	if err == nil {
		return ""
	}
	if errors.Is(err, netguard.ErrNotPublic) {
		return FailureNotPublic
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return classifyStatus(apiErr.StatusCode)
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) {
		return FailureUnreachable
	}
	return FailureOther
}

func classifyStatus(code int) FailureKind {
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return FailureRejected
	case code == http.StatusTooManyRequests:
		return FailureRateLimited
	case code == http.StatusNotFound || code == http.StatusBadRequest || code == http.StatusUnprocessableEntity:
		return FailureNotFound
	case code >= http.StatusInternalServerError:
		return FailureProviderError
	default:
		return FailureOther
	}
}
