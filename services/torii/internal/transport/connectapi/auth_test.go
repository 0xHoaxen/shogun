package connectapi_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/emptypb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpauth"
)

const (
	ownerEmail = "owner@example.com"
	sessionTTL = 24 * time.Hour
	probePath  = "/test.Probe/Do"
)

type harness struct {
	auth     *app.Auth
	sessions *apptest.MemSessions
	client   apiv1connect.AuthServiceClient
	probe    *connect.Client[emptypb.Empty, emptypb.Empty]
	identity chan authz.Identity
	now      *time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	sessions := apptest.NewMemSessions()
	auth, err := app.NewAuth(sessions,
		app.AuthConfig{AllowedEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	interceptor := connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{
		Auth: auth, Open: []string{apiv1connect.AuthServiceLogoutProcedure}, Log: log,
	})
	identity := make(chan authz.Identity, 1)
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewAuthServiceHandler(connectapi.NewAuthServer(auth, false, log), connect.WithInterceptors(interceptor)))
	mux.Handle(probePath, connect.NewUnaryHandler(probePath,
		func(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			id, _ := authz.FromContext(ctx)
			identity <- id
			return connect.NewResponse(&emptypb.Empty{}), nil
		}, connect.WithInterceptors(interceptor)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &harness{
		auth: auth, sessions: sessions, identity: identity, now: &now,
		client: apiv1connect.NewAuthServiceClient(srv.Client(), srv.URL),
		probe:  connect.NewClient[emptypb.Empty, emptypb.Empty](srv.Client(), srv.URL+probePath),
	}
}

func (h *harness) login(t *testing.T) string {
	t.Helper()
	token, _, err := h.auth.StartSession(context.Background(),
		app.Claims{Subject: "sub-1", Email: ownerEmail, EmailVerified: true, DisplayName: "Owner"})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	return token
}

func withCookie[T any](req *connect.Request[T], token string) *connect.Request[T] {
	if token != "" {
		req.Header().Set("Cookie", httpauth.SessionCookieName+"="+token)
	}
	return req
}

func reasonOf(err error) string {
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		return ""
	}
	for _, detail := range cerr.Details() {
		if value, valueErr := detail.Value(); valueErr == nil {
			if info, ok := value.(*errdetails.ErrorInfo); ok {
				return info.GetReason()
			}
		}
	}
	return ""
}

func TestGetSessionWithoutValidSessionIsUnauthenticated(t *testing.T) {
	tests := []struct {
		name  string
		token func(h *harness, t *testing.T) string
	}{
		{"no cookie", func(*harness, *testing.T) string { return "" }},
		{"unknown token", func(*harness, *testing.T) string { return "not-a-real-token" }},
		{"expired session", func(h *harness, t *testing.T) string {
			token := h.login(t)
			*h.now = h.now.Add(sessionTTL)
			return token
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			h := newHarness(t)
			token := tt.token(h, t)

			// Act
			_, err := h.client.GetSession(context.Background(), withCookie(connect.NewRequest(&apiv1.GetSessionRequest{}), token))

			// Assert
			if connect.CodeOf(err) != connect.CodeUnauthenticated {
				t.Fatalf("code = %v, want Unauthenticated (err %v)", connect.CodeOf(err), err)
			}
			if got := reasonOf(err); got != "UNAUTHENTICATED" {
				t.Fatalf("reason = %q, want UNAUTHENTICATED", got)
			}
		})
	}
}

func TestGetSessionReturnsTheOwner(t *testing.T) {
	// Arrange
	h := newHarness(t)
	token := h.login(t)

	// Act
	resp, err := h.client.GetSession(context.Background(), withCookie(connect.NewRequest(&apiv1.GetSessionRequest{}), token))
	// Assert
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	got := resp.Msg.GetSession()
	if got.GetEmail() != ownerEmail || got.GetDisplayName() != "Owner" || got.GetExpiresAt() == nil {
		t.Fatalf("session = %v", got)
	}
}

func TestGetSessionRefreshesTheCookieOnRenewal(t *testing.T) {
	// Arrange
	h := newHarness(t)
	token := h.login(t)
	*h.now = h.now.Add(sessionTTL/2 + time.Minute)

	// Act
	resp, err := h.client.GetSession(context.Background(), withCookie(connect.NewRequest(&apiv1.GetSessionRequest{}), token))
	// Assert
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	setCookie := resp.Header().Get("Set-Cookie")
	if !strings.HasPrefix(setCookie, httpauth.SessionCookieName+"="+token) || !strings.Contains(setCookie, "HttpOnly") {
		t.Fatalf("Set-Cookie = %q, want the renewed session cookie", setCookie)
	}
}

func TestInterceptorSetsTheOwnerIdentity(t *testing.T) {
	// Arrange
	h := newHarness(t)
	token := h.login(t)
	stored, _, err := h.auth.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Act
	_, callErr := h.probe.CallUnary(context.Background(), withCookie(connect.NewRequest(&emptypb.Empty{}), token))

	// Assert
	if callErr != nil {
		t.Fatalf("probe: %v", callErr)
	}
	got := <-h.identity
	if got.OwnerID != stored.OwnerID.String() || got.RequestID == "" {
		t.Fatalf("identity = %+v, want owner %s and a request id", got, stored.OwnerID)
	}
}

func TestLogoutEndsTheSessionAndClearsTheCookie(t *testing.T) {
	// Arrange
	h := newHarness(t)
	token := h.login(t)

	// Act
	resp, err := h.client.Logout(context.Background(), withCookie(connect.NewRequest(&apiv1.LogoutRequest{}), token))
	_, afterErr := h.client.GetSession(context.Background(), withCookie(connect.NewRequest(&apiv1.GetSessionRequest{}), token))

	// Assert
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if setCookie := resp.Header().Get("Set-Cookie"); !strings.Contains(setCookie, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q, want a cleared cookie", setCookie)
	}
	if connect.CodeOf(afterErr) != connect.CodeUnauthenticated || h.sessions.Len() != 0 {
		t.Fatalf("after logout: code %v, %d sessions left", connect.CodeOf(afterErr), h.sessions.Len())
	}
}

func TestLogoutSucceedsWithoutASession(t *testing.T) {
	// Arrange
	h := newHarness(t)

	// Act
	resp, err := h.client.Logout(context.Background(), connect.NewRequest(&apiv1.LogoutRequest{}))
	// Assert
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !strings.Contains(resp.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q, want a cleared cookie", resp.Header().Get("Set-Cookie"))
	}
}
