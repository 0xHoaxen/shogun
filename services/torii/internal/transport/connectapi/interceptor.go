package connectapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpauth"
)

const requestIDHeader = "X-Request-Id"

// Authenticator resolves a session token; app.Auth implements it.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (domain.Session, bool, error)
}

// InterceptorConfig configures the session interceptor.
type InterceptorConfig struct {
	Auth Authenticator
	// SecureCookies sets the Secure flag on a renewed session cookie.
	SecureCookies bool
	// Open lists procedures that skip authentication, such as Logout, which
	// must still clear the cookie when the session has already expired.
	Open []string
	Log  *slog.Logger
}

// NewSessionInterceptor authenticates every unary call from the session
// cookie. It puts the session and the owner's authz identity in the context,
// so downstream gRPC calls through grpcclient act for the owner, and it
// refreshes the cookie when the session was renewed.
func NewSessionInterceptor(cfg InterceptorConfig) connect.UnaryInterceptorFunc {
	open := make(map[string]struct{}, len(cfg.Open))
	for _, procedure := range cfg.Open {
		open[procedure] = struct{}{}
	}
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if _, skip := open[req.Spec().Procedure]; skip {
				return next(ctx, req)
			}
			token := TokenFromHeader(req.Header())
			session, renewed, err := cfg.Auth.Authenticate(ctx, token)
			if err != nil {
				return nil, cfg.authError(ctx, err)
			}
			ctx = WithSession(ctx, session)
			ctx = authz.WithIdentity(ctx, authz.Identity{
				OwnerID:   session.OwnerID.String(),
				RequestID: requestID(req.Header()),
			})
			resp, err := next(ctx, req)
			if err == nil && renewed {
				cookie := httpauth.SessionCookie(token, session.ExpiresAt, cfg.SecureCookies)
				resp.Header().Add("Set-Cookie", cookie.String())
			}
			return resp, err
		}
	}
}

func (cfg InterceptorConfig) authError(ctx context.Context, err error) error {
	if errors.Is(err, app.ErrUnauthenticated) {
		return newError(connect.CodeUnauthenticated, reasonUnauthenticated, "sign in required")
	}
	cfg.Log.ErrorContext(ctx, "authenticate session", slog.Any("error", err))
	return newError(connect.CodeInternal, reasonInternal, "something went wrong")
}

func requestID(h http.Header) string {
	if id := h.Get(requestIDHeader); id != "" {
		return id
	}
	return uuid.NewString()
}
