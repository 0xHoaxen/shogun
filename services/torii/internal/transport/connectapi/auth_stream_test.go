package connectapi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpauth"
)

// watchStream opens the probe stream and reads it to the end, returning the stream
// (for its headers), how many messages arrived and the error it ended with.
func (h *harness) watchStream(t *testing.T, token string) (*connect.ServerStreamForClient[emptypb.Empty], int, error) {
	t.Helper()
	stream, err := h.watch.CallServerStream(context.Background(), withCookie(connect.NewRequest(&emptypb.Empty{}), token))
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = stream.Close() }()
	received := 0
	for stream.Receive() {
		received++
	}
	return stream, received, stream.Err()
}

func TestStreamWithoutValidSessionIsUnauthenticatedAndNeverReachesTheHandler(t *testing.T) {
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
			h := newHarness(t)
			token := tt.token(h, t)

			_, received, err := h.watchStream(t, token)

			if connect.CodeOf(err) != connect.CodeUnauthenticated || reasonOf(err) != "UNAUTHENTICATED" {
				t.Fatalf("code %v, reason %q (err %v), want Unauthenticated", connect.CodeOf(err), reasonOf(err), err)
			}
			if received != 0 || len(h.identity) != 0 {
				t.Fatalf("the handler ran for an unauthenticated stream: %d messages, %d identities", received, len(h.identity))
			}
		})
	}
}

func TestStreamRunsAsTheOwner(t *testing.T) {
	h := newHarness(t)
	token := h.login(t)
	stored, _, err := h.auth.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	_, received, err := h.watchStream(t, token)

	if err != nil || received != 1 {
		t.Fatalf("received %d messages, err %v; want 1 and no error", received, err)
	}
	if got := <-h.identity; got.OwnerID != stored.OwnerID.String() || got.RequestID == "" {
		t.Fatalf("identity = %+v, want owner %s and a request id", got, stored.OwnerID)
	}
}

func TestStreamRefreshesTheCookieOnRenewal(t *testing.T) {
	h := newHarness(t)
	token := h.login(t)
	*h.now = h.now.Add(sessionTTL/2 + time.Minute)

	stream, _, err := h.watchStream(t, token)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	setCookie := stream.ResponseHeader().Get("Set-Cookie")
	if !strings.HasPrefix(setCookie, httpauth.SessionCookieName+"="+token) || !strings.Contains(setCookie, "HttpOnly") {
		t.Fatalf("Set-Cookie = %q, want the renewed session cookie", setCookie)
	}
}
