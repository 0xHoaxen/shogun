package connectapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpmw"
)

// twoUsers signs in two users and serves AuthService behind a per-user limit of
// two requests, with no refill, so the test needs no clock.
func twoUsers(t *testing.T) (client apiv1connect.AuthServiceClient, ada, bob string) {
	t.Helper()
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	auth, err := app.NewAuth(apptest.NewMemSessions(), apptest.NewMemUsers(),
		app.AuthConfig{AdminEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	ada, _, err = signIn(auth, "sub-ada", "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bob, _, err = signIn(auth, "sub-bob", "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	limiter := httpmw.NewRateLimiter(0, 2)
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewAuthServiceHandler(connectapi.NewAuthServer(auth, false, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Limiter: limiter, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return apiv1connect.NewAuthServiceClient(srv.Client(), srv.URL), ada, bob
}

func signIn(auth *app.Auth, sub, email string) (token string, owner string, err error) {
	token, session, err := auth.StartSession(context.Background(), app.Claims{Subject: sub, Email: email, EmailVerified: true})
	return token, session.OwnerID.String(), err
}

func getSession(client apiv1connect.AuthServiceClient, token string) error {
	_, err := client.GetSession(context.Background(), withCookie(connect.NewRequest(&apiv1.GetSessionRequest{}), token))
	return err
}

func TestOneUserOverTheirLimitDoesNotSlowDownAnother(t *testing.T) {
	// Arrange
	client, ada, bob := twoUsers(t)

	// Act: ada makes three requests against a limit of two; bob makes one.
	first, second, third := getSession(client, ada), getSession(client, ada), getSession(client, ada)
	bobs := getSession(client, bob)

	// Assert
	if first != nil || second != nil {
		t.Fatalf("ada's first two requests failed: %v, %v", first, second)
	}
	if connect.CodeOf(third) != connect.CodeResourceExhausted || reasonOf(third) != "RATE_LIMITED" {
		t.Fatalf("ada's third request: code %v reason %q, want ResourceExhausted RATE_LIMITED", connect.CodeOf(third), reasonOf(third))
	}
	if bobs != nil {
		t.Fatalf("bob was refused because ada used her limit: %v", bobs)
	}
}

func TestARequestWithoutASessionNeverSpendsAUsersLimit(t *testing.T) {
	client, ada, _ := twoUsers(t)

	for range 5 {
		if err := getSession(client, "not-a-session"); connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatalf("code %v, want Unauthenticated", connect.CodeOf(err))
		}
	}

	if err := getSession(client, ada); err != nil {
		t.Fatalf("ada's first request failed after strangers knocked: %v", err)
	}
}
