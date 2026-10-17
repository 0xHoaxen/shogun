package connectapi

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpauth"
)

// AuthBackend is the part of app.Auth that AuthServer uses.
type AuthBackend interface {
	EndSession(ctx context.Context, token string) error
	EndAllSessions(ctx context.Context, owner uuid.UUID) (int64, error)
	SetTimezone(ctx context.Context, owner uuid.UUID, name string) (domain.User, error)
}

// AuthServer implements shogun.api.v1.AuthService.
type AuthServer struct {
	auth   AuthBackend
	secure bool
	log    *slog.Logger
}

var _ apiv1connect.AuthServiceHandler = (*AuthServer)(nil)

// NewAuthServer returns an AuthServer. secure sets the Secure flag on the
// cookie that logout clears.
func NewAuthServer(auth AuthBackend, secure bool, log *slog.Logger) *AuthServer {
	return &AuthServer{auth: auth, secure: secure, log: log}
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
		Email:        session.Email,
		DisplayName:  session.DisplayName,
		ExpiresAt:    timestamppb.New(session.ExpiresAt),
		Role:         string(session.Role),
		GmailAllowed: session.GmailAllowed,
		Timezone:     session.Timezone,
	}}), nil
}

// Logout deletes the session and clears its cookie. It succeeds without a
// session so a stale cookie can always be cleared.
func (s *AuthServer) Logout(
	ctx context.Context, req *connect.Request[apiv1.LogoutRequest],
) (*connect.Response[apiv1.LogoutResponse], error) {
	if token := TokenFromHeader(req.Header()); token != "" {
		if err := s.auth.EndSession(ctx, token); err != nil {
			s.log.ErrorContext(ctx, "end session", slog.Any("error", err))
			return nil, newError(connect.CodeInternal, reasonInternal, "logout failed")
		}
	}
	resp := connect.NewResponse(&apiv1.LogoutResponse{})
	resp.Header().Add("Set-Cookie", httpauth.ClearedSessionCookie(s.secure).String())
	return resp, nil
}

// LogoutEverywhere ends every session of the signed-in user and clears this
// browser's cookie.
func (s *AuthServer) LogoutEverywhere(
	ctx context.Context, _ *connect.Request[apiv1.LogoutEverywhereRequest],
) (*connect.Response[apiv1.LogoutEverywhereResponse], error) {
	session, ok := SessionFromContext(ctx)
	if !ok {
		return nil, newError(connect.CodeUnauthenticated, reasonUnauthenticated, "sign in required")
	}
	ended, err := s.auth.EndAllSessions(ctx, session.OwnerID)
	if err != nil {
		s.log.ErrorContext(ctx, "end all sessions", slog.Any("error", err))
		return nil, newError(connect.CodeInternal, reasonInternal, "logout failed")
	}
	resp := connect.NewResponse(&apiv1.LogoutEverywhereResponse{Ended: ended})
	resp.Header().Add("Set-Cookie", httpauth.ClearedSessionCookie(s.secure).String())
	return resp, nil
}

// SetTimezone changes the signed-in user's time zone.
func (s *AuthServer) SetTimezone(
	ctx context.Context, req *connect.Request[apiv1.SetTimezoneRequest],
) (*connect.Response[apiv1.SetTimezoneResponse], error) {
	session, ok := SessionFromContext(ctx)
	if !ok {
		return nil, newError(connect.CodeUnauthenticated, reasonUnauthenticated, "sign in required")
	}
	user, err := s.auth.SetTimezone(ctx, session.OwnerID, req.Msg.GetTimezone())
	if errors.Is(err, app.ErrInvalidTimezone) {
		return nil, newError(connect.CodeInvalidArgument, reasonInvalidTimezone, "that is not a time zone name such as Europe/Berlin")
	}
	if err != nil {
		s.log.ErrorContext(ctx, "set timezone", slog.Any("error", err))
		return nil, newError(connect.CodeInternal, reasonInternal, "could not change the time zone")
	}
	return connect.NewResponse(&apiv1.SetTimezoneResponse{Timezone: user.Timezone}), nil
}
