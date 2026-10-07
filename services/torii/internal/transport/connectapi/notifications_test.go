package connectapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

const streamWait = 5 * time.Second

// fakeSub is the client side of a taiko Subscribe stream, fed by the test.
type fakeSub struct {
	grpc.ClientStream
	ctx   context.Context
	items chan subItem
}

type subItem struct {
	n   *taikov1.Notification
	err error
}

func (s *fakeSub) Context() context.Context { return s.ctx }

func (s *fakeSub) Recv() (*taikov1.SubscribeResponse, error) {
	select {
	case item := <-s.items:
		if item.err != nil {
			return nil, item.err
		}
		return &taikov1.SubscribeResponse{Notification: item.n}, nil
	case <-s.ctx.Done():
		return nil, status.FromContextError(s.ctx.Err()).Err()
	}
}

// fakeTaiko answers from what a test sets and records what it was asked.
type fakeTaiko struct {
	mu          sync.Mutex
	listReq     *taikov1.ListRequest
	listResp    *taikov1.ListResponse
	listErr     error
	markIDs     []string
	markAll     int
	subReq      *taikov1.SubscribeRequest
	subIdentity authz.Identity
	subErr      error
	sub         *fakeSub
	subscribed  chan struct{}
	settings    settingsFake
}

func (f *fakeTaiko) List(_ context.Context, in *taikov1.ListRequest, _ ...grpc.CallOption) (*taikov1.ListResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listReq = in
	return f.listResp, f.listErr
}

func (f *fakeTaiko) MarkRead(_ context.Context, in *taikov1.MarkReadRequest, _ ...grpc.CallOption) (*taikov1.MarkReadResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markIDs = in.GetIds()
	return &taikov1.MarkReadResponse{}, nil
}

func (f *fakeTaiko) MarkAllRead(context.Context, *taikov1.MarkAllReadRequest, ...grpc.CallOption) (*taikov1.MarkAllReadResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markAll++
	return &taikov1.MarkAllReadResponse{}, nil
}

func (f *fakeTaiko) Subscribe(ctx context.Context, in *taikov1.SubscribeRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[taikov1.SubscribeResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subReq = in
	f.subIdentity, _ = authz.FromContext(ctx)
	if f.subErr != nil {
		return nil, f.subErr
	}
	f.sub = &fakeSub{ctx: ctx, items: make(chan subItem, 8)}
	close(f.subscribed)
	return f.sub, nil
}

type notificationsHarness struct {
	taiko  *fakeTaiko
	client apiv1connect.NotificationsServiceClient
	token  string
	owner  string
}

func newNotificationsHarness(t *testing.T, opts ...connectapi.NotificationsOption) *notificationsHarness {
	t.Helper()
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	auth, err := app.NewAuth(apptest.NewMemSessions(),
		app.AuthConfig{AllowedEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	token, session, err := auth.StartSession(context.Background(), app.Claims{Subject: "sub-1", Email: ownerEmail, EmailVerified: true})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	taiko := &fakeTaiko{subscribed: make(chan struct{})}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewNotificationsServiceHandler(connectapi.NewNotificationsServer(taiko, log, opts...),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &notificationsHarness{
		taiko: taiko, token: token, owner: session.OwnerID.String(),
		client: apiv1connect.NewNotificationsServiceClient(srv.Client(), srv.URL),
	}
}

// stream starts the Stream RPC and waits until taiko was asked to subscribe.
// Connect's client call returns only once the server sends something, so the
// call runs in the background; open waits for it and returns the stream.
func (h *notificationsHarness) stream(ctx context.Context, t *testing.T, lastSeen string) (open func() *connect.ServerStreamForClient[apiv1.StreamResponse]) {
	t.Helper()
	type result struct {
		s   *connect.ServerStreamForClient[apiv1.StreamResponse]
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := h.client.Stream(ctx, withCookie(connect.NewRequest(&apiv1.StreamRequest{LastSeenId: lastSeen}), h.token))
		done <- result{s, err}
	}()
	select {
	case <-h.taiko.subscribed:
	case <-time.After(streamWait):
		t.Fatal("torii never subscribed to taiko")
	}
	return func() *connect.ServerStreamForClient[apiv1.StreamResponse] {
		t.Helper()
		select {
		case r := <-done:
			if r.err != nil {
				t.Fatalf("Stream: %v", r.err)
			}
			return r.s
		case <-time.After(streamWait):
			t.Fatal("the stream never opened")
			return nil
		}
	}
}

func TestListNotificationsMapsTheResponseAndPassesTheArguments(t *testing.T) {
	h := newNotificationsHarness(t)
	created, read := timestamppb.New(time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)), timestamppb.New(time.Date(2026, 10, 7, 8, 5, 0, 0, time.UTC))
	h.taiko.listResp = &taikov1.ListResponse{
		Notifications: []*taikov1.Notification{{
			Id: "n1", Type: taikov1.NotificationType_NOTIFICATION_TYPE_DRAFT_READY, Title: "Cover letter ready",
			Body: "Review it.", Link: "/drafts/d1", CreatedAt: created, ReadAt: read,
		}},
		NextPageToken: "next", UnreadCount: 7,
	}

	resp, err := h.client.ListNotifications(context.Background(), withCookie(connect.NewRequest(&apiv1.ListNotificationsRequest{
		UnreadOnly: true, PageSize: 20, PageToken: "tok",
	}), h.token))
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	n := resp.Msg.GetNotifications()[0]
	if n.GetId() != "n1" || n.GetType() != apiv1.NotificationType_NOTIFICATION_TYPE_DRAFT_READY || n.GetTitle() != "Cover letter ready" ||
		n.GetBody() != "Review it." || n.GetLink() != "/drafts/d1" || !n.GetCreatedAt().AsTime().Equal(created.AsTime()) ||
		!n.GetReadAt().AsTime().Equal(read.AsTime()) {
		t.Fatalf("got %+v", n)
	}
	if resp.Msg.GetNextPageToken() != "next" || resp.Msg.GetUnreadCount() != 7 {
		t.Fatalf("got token %q and unread %d", resp.Msg.GetNextPageToken(), resp.Msg.GetUnreadCount())
	}
	if got := h.taiko.listReq; !got.GetUnreadOnly() || got.GetPageSize() != 20 || got.GetPageToken() != "tok" {
		t.Fatalf("taiko was asked %+v", got)
	}
}

func TestEveryTaikoNotificationTypeMapsToTheSameAPIType(t *testing.T) {
	h := newNotificationsHarness(t)
	var want []apiv1.NotificationType
	var types []*taikov1.Notification
	for value, name := range taikov1.NotificationType_name {
		types = append(types, &taikov1.Notification{Id: name, Type: taikov1.NotificationType(value)})
		want = append(want, apiv1.NotificationType(apiv1.NotificationType_value[name]))
	}
	h.taiko.listResp = &taikov1.ListResponse{Notifications: types}

	resp, err := h.client.ListNotifications(context.Background(), withCookie(connect.NewRequest(&apiv1.ListNotificationsRequest{}), h.token))
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	for i, n := range resp.Msg.GetNotifications() {
		if n.GetType() != want[i] || (n.GetType() == 0) != (n.GetId() == "NOTIFICATION_TYPE_UNSPECIFIED") {
			t.Errorf("%s mapped to %s", n.GetId(), n.GetType())
		}
	}
}

func TestListNotificationsKeepsTheReasonOfARefusal(t *testing.T) {
	h := newNotificationsHarness(t)
	h.taiko.listErr = withInfo(codes.InvalidArgument, "INVALID_PAGE_TOKEN")

	_, err := h.client.ListNotifications(context.Background(), withCookie(connect.NewRequest(&apiv1.ListNotificationsRequest{}), h.token))

	if code, reason := codeAndReason(t, err); code != connect.CodeInvalidArgument || reason != "INVALID_PAGE_TOKEN" {
		t.Fatalf("got %v %q, want InvalidArgument INVALID_PAGE_TOKEN", code, reason)
	}
}

func TestMarkNotificationsReadAndMarkAllPassThrough(t *testing.T) {
	h := newNotificationsHarness(t)

	_, errRead := h.client.MarkNotificationsRead(context.Background(),
		withCookie(connect.NewRequest(&apiv1.MarkNotificationsReadRequest{Ids: []string{"a", "b"}}), h.token))
	_, errAll := h.client.MarkAllNotificationsRead(context.Background(),
		withCookie(connect.NewRequest(&apiv1.MarkAllNotificationsReadRequest{}), h.token))

	if errRead != nil || errAll != nil {
		t.Fatalf("errors %v and %v", errRead, errAll)
	}
	if len(h.taiko.markIDs) != 2 || h.taiko.markIDs[0] != "a" || h.taiko.markAll != 1 {
		t.Fatalf("taiko got ids %v and %d mark-all calls", h.taiko.markIDs, h.taiko.markAll)
	}
}

func TestCallsWithoutASessionAreRefused(t *testing.T) {
	h := newNotificationsHarness(t)

	_, err := h.client.ListNotifications(context.Background(), connect.NewRequest(&apiv1.ListNotificationsRequest{}))
	s, streamErr := h.client.Stream(context.Background(), connect.NewRequest(&apiv1.StreamRequest{}))
	if streamErr == nil {
		for s.Receive() {
		}
		streamErr = s.Err()
	}

	if connect.CodeOf(err) != connect.CodeUnauthenticated || connect.CodeOf(streamErr) != connect.CodeUnauthenticated {
		t.Fatalf("got %v and %v, want Unauthenticated for both", err, streamErr)
	}
	if h.taiko.listReq != nil || h.taiko.subReq != nil {
		t.Fatal("taiko was called without a session")
	}
}

func TestStreamForwardsNotificationsAsTheOwnerFromTheLastSeenID(t *testing.T) {
	h := newNotificationsHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), streamWait)
	defer cancel()
	open := h.stream(ctx, t, "last-id")
	h.taiko.sub.items <- subItem{n: &taikov1.Notification{Id: "n1", Title: "one", Type: taikov1.NotificationType_NOTIFICATION_TYPE_OFFER}}
	h.taiko.sub.items <- subItem{n: &taikov1.Notification{Id: "n2", Title: "two"}}
	s := open()

	first := s.Receive()
	got1 := s.Msg().GetNotification()
	second := s.Receive()
	got2 := s.Msg().GetNotification()

	if !first || !second || got1.GetId() != "n1" || got2.GetId() != "n2" ||
		got1.GetType() != apiv1.NotificationType_NOTIFICATION_TYPE_OFFER || got1.GetTitle() != "one" {
		t.Fatalf("got %v %v and %v %v", first, got1, second, got2)
	}
	if h.taiko.subReq.GetAfterId() != "last-id" {
		t.Fatalf("taiko was asked to replay after %q, want last-id", h.taiko.subReq.GetAfterId())
	}
	if h.taiko.subIdentity.OwnerID != h.owner {
		t.Fatalf("subscribed as %q, want the session's owner %s", h.taiko.subIdentity.OwnerID, h.owner)
	}
}

func TestStreamEndsWithUnavailableWhenTaikoResetsIt(t *testing.T) {
	h := newNotificationsHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), streamWait)
	defer cancel()
	open := h.stream(ctx, t, "")
	h.taiko.sub.items <- subItem{err: withInfo(codes.Unavailable, "STREAM_RESET")}
	s := open()

	for s.Receive() {
	}

	if connect.CodeOf(s.Err()) != connect.CodeUnavailable {
		t.Fatalf("got %v, want Unavailable so the browser reconnects", s.Err())
	}
}

func TestStreamEndsWithUnavailableWhenTaikoClosesIt(t *testing.T) {
	h := newNotificationsHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), streamWait)
	defer cancel()
	open := h.stream(ctx, t, "")
	h.taiko.sub.items <- subItem{err: io.EOF}
	s := open()

	for s.Receive() {
	}

	if code, reason := codeAndReason(t, s.Err()); code != connect.CodeUnavailable || reason != "STREAM_RESET" {
		t.Fatalf("got %v %q, want Unavailable STREAM_RESET", code, reason)
	}
}

func TestStreamEndsCleanlyAtItsMaximumAgeAndStopsListeningToTaiko(t *testing.T) {
	h := newNotificationsHarness(t, connectapi.WithMaxStreamAge(100*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), streamWait)
	defer cancel()
	s := h.stream(ctx, t, "")()

	for s.Receive() {
	}

	if s.Err() != nil {
		t.Fatalf("got %v, want a clean end so the browser reconnects", s.Err())
	}
	select {
	case <-h.taiko.sub.ctx.Done():
	case <-time.After(streamWait):
		t.Fatal("the subscription to taiko was left open")
	}
}

func TestStreamStopsListeningToTaikoWhenTheBrowserLeaves(t *testing.T) {
	h := newNotificationsHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.stream(ctx, t, "")

	cancel()

	select {
	case <-h.taiko.sub.ctx.Done():
	case <-time.After(streamWait):
		t.Fatal("the subscription to taiko was left open")
	}
}

func TestStreamReportsATaikoThatCannotBeReached(t *testing.T) {
	h := newNotificationsHarness(t)
	h.taiko.subErr = status.Error(codes.Unavailable, "taiko is down")
	s, err := h.client.Stream(context.Background(), withCookie(connect.NewRequest(&apiv1.StreamRequest{}), h.token))
	if err == nil {
		for s.Receive() {
		}
		err = s.Err()
	}

	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("got %v, want Unavailable", err)
	}
}
