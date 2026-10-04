// Package connectapi serves torii's browser API over ConnectRPC: the session
// interceptor, the error helper and the AuthService handlers.
package connectapi

import (
	"context"
	"net/http"

	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpauth"
)

type sessionKey struct{}

// WithSession returns ctx carrying the signed-in session.
func WithSession(ctx context.Context, s domain.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFromContext returns the session the interceptor resolved, if any.
func SessionFromContext(ctx context.Context) (domain.Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(domain.Session)
	return s, ok
}

// TokenFromHeader returns the session token in the request's Cookie header, or
// "" when there is none.
func TokenFromHeader(h http.Header) string {
	cookie, err := (&http.Request{Header: h}).Cookie(httpauth.SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
