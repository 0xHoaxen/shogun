package grpc_test

import (
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
)

func (h *harness) list(t *testing.T, req *taikov1.ListRequest) *taikov1.ListResponse {
	t.Helper()
	res, err := h.client.List(h.ctx(t), req)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return res
}

func TestListPagesNewestFirstAndCountsAllUnread(t *testing.T) {
	h := newHarness(t)
	for range 3 {
		h.draftReady(t, h.owner)
	}
	h.draftReady(t, uuid.NewString())

	page1 := h.list(t, &taikov1.ListRequest{PageSize: 2})
	page2 := h.list(t, &taikov1.ListRequest{PageSize: 2, PageToken: page1.GetNextPageToken()})

	if len(page1.GetNotifications()) != 2 || len(page2.GetNotifications()) != 1 {
		t.Fatalf("page sizes %d and %d, want 2 and 1", len(page1.GetNotifications()), len(page2.GetNotifications()))
	}
	if page1.GetNextPageToken() == "" || page2.GetNextPageToken() != "" {
		t.Fatalf("tokens %q and %q: only the first page has a next one", page1.GetNextPageToken(), page2.GetNextPageToken())
	}
	if page1.GetUnreadCount() != 3 || page2.GetUnreadCount() != 3 {
		t.Fatalf("unread %d and %d, want 3: the count covers every page and no other owner", page1.GetUnreadCount(), page2.GetUnreadCount())
	}
	first := page1.GetNotifications()[0]
	if first.GetType() != taikov1.NotificationType_NOTIFICATION_TYPE_DRAFT_READY || first.GetTitle() != "Cover letter ready" ||
		first.GetCreatedAt() == nil || first.GetReadAt() != nil {
		t.Fatalf("got %+v", first)
	}
	if page1.GetNotifications()[0].GetId() <= page1.GetNotifications()[1].GetId() {
		t.Fatalf("not newest first: %s then %s", page1.GetNotifications()[0].GetId(), page1.GetNotifications()[1].GetId())
	}
}

func TestListUnreadOnlyHidesReadNotifications(t *testing.T) {
	h := newHarness(t)
	h.draftReady(t, h.owner)
	h.draftReady(t, h.owner)
	all := h.list(t, &taikov1.ListRequest{})
	if _, err := h.client.MarkRead(h.ctx(t), &taikov1.MarkReadRequest{Ids: []string{all.GetNotifications()[0].GetId()}}); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	unread := h.list(t, &taikov1.ListRequest{UnreadOnly: true})
	everything := h.list(t, &taikov1.ListRequest{})

	if len(unread.GetNotifications()) != 1 || unread.GetUnreadCount() != 1 || len(everything.GetNotifications()) != 2 {
		t.Fatalf("unread %d (count %d), all %d; want 1 (1) and 2", len(unread.GetNotifications()), unread.GetUnreadCount(), len(everything.GetNotifications()))
	}
	if everything.GetNotifications()[0].GetReadAt() == nil {
		t.Fatalf("the newest one was marked read, got %+v", everything.GetNotifications()[0])
	}
}

func TestListRejectsAMalformedPageToken(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.List(h.ctx(t), &taikov1.ListRequest{PageToken: "%%%"})

	requireStatus(t, err, codes.InvalidArgument, "INVALID_PAGE_TOKEN")
}

func TestMarkReadOnlyChangesTheOwnersNotifications(t *testing.T) {
	h := newHarness(t)
	other := uuid.NewString()
	h.draftReady(t, h.owner)
	h.draftReady(t, other)
	mine := h.list(t, &taikov1.ListRequest{}).GetNotifications()[0].GetId()
	theirs, err := h.client.List(h.ctxFor(t, other), &taikov1.ListRequest{})
	if err != nil {
		t.Fatalf("list other: %v", err)
	}

	_, err = h.client.MarkRead(h.ctx(t), &taikov1.MarkReadRequest{Ids: []string{mine, theirs.GetNotifications()[0].GetId(), uuid.NewString()}})
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	after, _ := h.client.List(h.ctxFor(t, other), &taikov1.ListRequest{})
	if h.list(t, &taikov1.ListRequest{}).GetUnreadCount() != 0 || after.GetUnreadCount() != 1 {
		t.Fatalf("unread %d for me and %d for them, want 0 and 1", h.list(t, &taikov1.ListRequest{}).GetUnreadCount(), after.GetUnreadCount())
	}
}

func TestMarkReadAcceptsNoIdsAndRejectsABadOne(t *testing.T) {
	h := newHarness(t)

	_, errEmpty := h.client.MarkRead(h.ctx(t), &taikov1.MarkReadRequest{})
	_, errBad := h.client.MarkRead(h.ctx(t), &taikov1.MarkReadRequest{Ids: []string{uuid.NewString(), "nope"}})

	if errEmpty != nil {
		t.Fatalf("no ids: %v", errEmpty)
	}
	requireStatus(t, errBad, codes.InvalidArgument, "INVALID_ID")
}

func TestMarkAllReadClearsTheUnreadCount(t *testing.T) {
	h := newHarness(t)
	other := uuid.NewString()
	h.draftReady(t, h.owner)
	h.draftReady(t, h.owner)
	h.draftReady(t, other)

	_, err := h.client.MarkAllRead(h.ctx(t), &taikov1.MarkAllReadRequest{})
	if err != nil {
		t.Fatalf("mark all read: %v", err)
	}
	theirs, _ := h.client.List(h.ctxFor(t, other), &taikov1.ListRequest{})
	if got := h.list(t, &taikov1.ListRequest{}).GetUnreadCount(); got != 0 || theirs.GetUnreadCount() != 1 {
		t.Fatalf("unread %d for me and %d for them, want 0 and 1", got, theirs.GetUnreadCount())
	}
}

func TestCallsWithoutAnIdentityAreRefused(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.List(t.Context(), &taikov1.ListRequest{})

	requireStatus(t, err, codes.Unauthenticated, "")
}
