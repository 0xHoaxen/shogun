package connectapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

type fakeTsubame struct {
	connect  func(*tsubamev1.ConnectAccountRequest) (*tsubamev1.ConnectAccountResponse, error)
	complete func(*tsubamev1.CompleteConnectRequest) (*tsubamev1.CompleteConnectResponse, error)
	sent     *tsubamev1.CompleteConnectRequest
	provider tsubamev1.MailProvider
}

func (f *fakeTsubame) ConnectAccount(_ context.Context, in *tsubamev1.ConnectAccountRequest, _ ...grpc.CallOption) (*tsubamev1.ConnectAccountResponse, error) {
	f.provider = in.GetProvider()
	return f.connect(in)
}

func (f *fakeTsubame) CompleteConnect(_ context.Context, in *tsubamev1.CompleteConnectRequest, _ ...grpc.CallOption) (*tsubamev1.CompleteConnectResponse, error) {
	f.sent = in
	return f.complete(in)
}

func newMailHarness(t *testing.T) (*fakeTsubame, apiv1connect.MailServiceClient, string) {
	t.Helper()
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	auth, err := app.NewAuth(apptest.NewMemSessions(),
		app.AuthConfig{AllowedEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	token, _, err := auth.StartSession(context.Background(), app.Claims{Subject: "sub-1", Email: ownerEmail, EmailVerified: true})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	fake := &fakeTsubame{}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewMailServiceHandler(connectapi.NewMailServer(fake, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fake, apiv1connect.NewMailServiceClient(srv.Client(), srv.URL), token
}

func TestConnectAccountAsksTsubameForGmailAndReturnsTheURL(t *testing.T) {
	fake, client, token := newMailHarness(t)
	fake.connect = func(*tsubamev1.ConnectAccountRequest) (*tsubamev1.ConnectAccountResponse, error) {
		return &tsubamev1.ConnectAccountResponse{AuthUrl: "https://accounts.example/auth?state=s"}, nil
	}

	resp, err := client.ConnectAccount(context.Background(), withCookie(connect.NewRequest(&apiv1.ConnectAccountRequest{}), token))

	if err != nil || resp.Msg.GetAuthUrl() != "https://accounts.example/auth?state=s" || fake.provider != tsubamev1.MailProvider_MAIL_PROVIDER_GMAIL {
		t.Fatalf("got %v, %v, provider %v", resp.Msg, err, fake.provider)
	}
}

func TestCompleteConnectPassesCodeAndStateAndReturnsOnlyTheAddress(t *testing.T) {
	fake, client, token := newMailHarness(t)
	fake.complete = func(*tsubamev1.CompleteConnectRequest) (*tsubamev1.CompleteConnectResponse, error) {
		return &tsubamev1.CompleteConnectResponse{Account: &tsubamev1.Account{Id: "a1", Address: "me@example.com"}}, nil
	}

	resp, err := client.CompleteConnect(context.Background(), withCookie(connect.NewRequest(&apiv1.CompleteConnectRequest{Code: "4/code", State: "st"}), token))

	if err != nil || resp.Msg.GetAddress() != "me@example.com" || fake.sent.GetCode() != "4/code" || fake.sent.GetState() != "st" {
		t.Fatalf("got %v, %v, sent %v", resp.Msg, err, fake.sent)
	}
}

func TestCompleteConnectKeepsTheReasonOfARefusalAndNeverEchoesTheCode(t *testing.T) {
	fake, client, token := newMailHarness(t)
	fake.complete = func(*tsubamev1.CompleteConnectRequest) (*tsubamev1.CompleteConnectResponse, error) {
		return nil, withInfo(codes.InvalidArgument, "INVALID_STATE")
	}

	_, err := client.CompleteConnect(context.Background(), withCookie(connect.NewRequest(&apiv1.CompleteConnectRequest{Code: "4/secret-code", State: "st"}), token))

	code, reason := codeAndReason(t, err)
	if code != connect.CodeInvalidArgument || reason != "INVALID_STATE" {
		t.Fatalf("got %s %q", code, reason)
	}
	if err != nil && strings.Contains(err.Error(), "4/secret-code") {
		t.Fatalf("the code leaked into the error: %v", err)
	}
}

func TestMailCallsRequireASessionAndHideInternals(t *testing.T) {
	fake, client, token := newMailHarness(t)
	fake.connect = func(*tsubamev1.ConnectAccountRequest) (*tsubamev1.ConnectAccountResponse, error) {
		return nil, status.Error(codes.Internal, "secret internals")
	}

	_, unauthenticated := client.ConnectAccount(context.Background(), connect.NewRequest(&apiv1.ConnectAccountRequest{}))
	_, internal := client.ConnectAccount(context.Background(), withCookie(connect.NewRequest(&apiv1.ConnectAccountRequest{}), token))

	if code, _ := codeAndReason(t, unauthenticated); code != connect.CodeUnauthenticated {
		t.Fatalf("got %v", unauthenticated)
	}
	if code, _ := codeAndReason(t, internal); code != connect.CodeInternal || strings.Contains(internal.Error(), "secret internals") {
		t.Fatalf("got %v", internal)
	}
}
