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

// NewSessionInterceptor authenticates every unary and streaming call from the
// session cookie. It puts the session and the owner's authz identity in the
// context, so downstream gRPC calls through grpcclient act for the owner, and
// it refreshes the cookie when the session was renewed.
//
// A stream is authenticated once, when it opens. It is not checked again while
// it runs, so a stream can outlive the session that opened it; handlers keep
// that in mind and end streams after a bounded time.
func NewSessionInterceptor(cfg InterceptorConfig) connect.Interceptor {
	open := make(map[string]struct{}, len(cfg.Open))
	for _, procedure := range cfg.Open {
		open[procedure] = struct{}{}
	}
	return &sessionInterceptor{cfg: cfg, open: open}
}

type sessionInterceptor struct {
	cfg  InterceptorConfig
	open map[string]struct{}
}

// authenticate resolves the session behind header and returns a context that
// carries it. renew is the cookie to send back when the session was renewed.
func (i *sessionInterceptor) authenticate(ctx context.Context, header http.Header) (_ context.Context, renew *http.Cookie, err error) {
	token := TokenFromHeader(header)
	session, renewed, err := i.cfg.Auth.Authenticate(ctx, token)
	if err != nil {
		return nil, nil, i.cfg.authError(ctx, err)
	}
	ctx = WithSession(ctx, session)
	ctx = authz.WithIdentity(ctx, authz.Identity{
		OwnerID:   session.OwnerID.String(),
		RequestID: requestID(header),
	})
	if renewed {
		renew = httpauth.SessionCookie(token, session.ExpiresAt, i.cfg.SecureCookies)
	}
	return ctx, renew, nil
}

func (i *sessionInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if _, skip := i.open[req.Spec().Procedure]; skip {
			return next(ctx, req)
		}
		ctx, renew, err := i.authenticate(ctx, req.Header())
		if err != nil {
			return nil, err
		}
		resp, err := next(ctx, req)
		if err == nil && renew != nil {
			resp.Header().Add("Set-Cookie", renew.String())
		}
		return resp, err
	}
}

// WrapStreamingClient leaves outgoing streams alone: torii only serves them.
func (i *sessionInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *sessionInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if _, skip := i.open[conn.Spec().Procedure]; skip {
			return next(ctx, conn)
		}
		ctx, renew, err := i.authenticate(ctx, conn.RequestHeader())
		if err != nil {
			return err
		}
		if renew != nil {
			// Headers go out with the first message, so the cookie is set now.
			conn.ResponseHeader().Add("Set-Cookie", renew.String())
		}
		return next(ctx, conn)
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
