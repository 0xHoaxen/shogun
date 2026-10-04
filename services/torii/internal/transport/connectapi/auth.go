package connectapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpauth"
)

// SessionEnder ends a session; app.Auth implements it.
type SessionEnder interface {
	EndSession(ctx context.Context, token string) error
}

// AuthServer implements shogun.api.v1.AuthService.
type AuthServer struct {
	ender  SessionEnder
	secure bool
	log    *slog.Logger
}

var _ apiv1connect.AuthServiceHandler = (*AuthServer)(nil)

// NewAuthServer returns an AuthServer. secure sets the Secure flag on the
// cookie that logout clears.
func NewAuthServer(ender SessionEnder, secure bool, log *slog.Logger) *AuthServer {
	return &AuthServer{ender: ender, secure: secure, log: log}
}

// GetSession returns the signed-in owner. The interceptor has already
// rejected calls without a session.
func (s *AuthServer) GetSession(
	ctx context.Context, _ *connect.Request[apiv1.GetSessionRequest],
) (*connect.Response[apiv1.GetSessionResponse], error) {
	session, ok := SessionFromContext(ctx)
	if !ok {
		return nil, newError(connect.CodeUnauthenticated, reasonUnauthenticated, "sign in required")
	}
	return connect.NewResponse(&apiv1.GetSessionResponse{Session: &apiv1.Session{
		Email:       session.Email,
		DisplayName: session.DisplayName,
		ExpiresAt:   timestamppb.New(session.ExpiresAt),
	}}), nil
}

// Logout deletes the session and clears its cookie. It succeeds without a
// session so a stale cookie can always be cleared.
func (s *AuthServer) Logout(
	ctx context.Context, req *connect.Request[apiv1.LogoutRequest],
) (*connect.Response[apiv1.LogoutResponse], error) {
	if token := TokenFromHeader(req.Header()); token != "" {
		if err := s.ender.EndSession(ctx, token); err != nil {
			s.log.ErrorContext(ctx, "end session", slog.Any("error", err))
			return nil, newError(connect.CodeInternal, reasonInternal, "logout failed")
		}
	}
	resp := connect.NewResponse(&apiv1.LogoutResponse{})
	resp.Header().Add("Set-Cookie", httpauth.ClearedSessionCookie(s.secure).String())
	return resp, nil
}
