package grpc_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
)

func hasCode(err error, code codes.Code) bool {
	st, ok := status.FromError(err)
	return ok && st.Code() == code
}

func TestLogActivityWritesExactlyOneEventNamingTheOwner(t *testing.T) {
	h := newHarness(t)
	item := h.addItem(t, "go")

	res, err := h.client.LogActivity(h.ctx(t), &dojov1.LogActivityRequest{
		ItemId: item.GetId(), Summary: " read chapter 3 ", Minutes: 45, Tags: []string{"Go", "go", "books"},
	})
	if err != nil {
		t.Fatalf("LogActivity: %v", err)
	}
	got := res.GetActivity()
	if got.GetSummary() != "read chapter 3" || got.GetMinutes() != 45 || got.GetOccurredOn() != "2026-10-07" || got.GetItemId() != item.GetId() {
		t.Fatalf("activity = %+v", got)
	}
	if len(got.GetTags()) != 2 || got.GetTags()[0] != "go" || got.GetTags()[1] != "books" {
		t.Fatalf("tags = %v, want the cleaned list", got.GetTags())
	}
	if h.outboxCount(t, "") != 1 || h.outboxCount(t, "learning.activity_added") != 1 {
		t.Fatalf("outbox rows: total %d, activity_added %d; want exactly one", h.outboxCount(t, ""), h.outboxCount(t, "learning.activity_added"))
	}
	var payload dojov1.LearningActivityAdded
	h.payloadOf(t, "learning.activity_added", &payload)
	if payload.GetActivityId() != got.GetId() || payload.GetItemId() != item.GetId() || payload.GetOwnerId() != h.owner ||
		payload.GetSummary() != "read chapter 3" {
		t.Fatalf("payload = %+v", &payload)
	}
}

func TestLogActivityWithoutAnItemIsFine(t *testing.T) {
	h := newHarness(t)

	res, err := h.client.LogActivity(h.ctx(t), &dojov1.LogActivityRequest{Summary: "listened to a podcast", OccurredOn: "2026-10-05"})

	if err != nil || res.GetActivity().GetItemId() != "" || res.GetActivity().GetOccurredOn() != "2026-10-05" {
		t.Fatalf("activity = %+v, %v", res.GetActivity(), err)
	}
}

func TestLogActivityRefusesBadInputAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	other := h.addItemFor(t, uuid.NewString(), "someone else's")
	tests := []struct {
		name   string
		req    *dojov1.LogActivityRequest
		code   codes.Code
		reason string
	}{
		{"no summary", &dojov1.LogActivityRequest{}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"future date", &dojov1.LogActivityRequest{Summary: "x", OccurredOn: "2026-10-08"}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"unreadable date", &dojov1.LogActivityRequest{Summary: "x", OccurredOn: "yesterday"}, codes.InvalidArgument, "INVALID_DATE"},
		{"bad item id", &dojov1.LogActivityRequest{Summary: "x", ItemId: "nope"}, codes.InvalidArgument, "INVALID_ID"},
		{"unknown item", &dojov1.LogActivityRequest{Summary: "x", ItemId: uuid.NewString()}, codes.NotFound, "ITEM_NOT_FOUND"},
		{"another owner's item", &dojov1.LogActivityRequest{Summary: "x", ItemId: other.GetId()}, codes.NotFound, "ITEM_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.LogActivity(h.ctx(t), tt.req)

			requireStatus(t, err, tt.code, tt.reason)
		})
	}
	if n := h.outboxCount(t, ""); n != 0 {
		t.Fatalf("outbox rows = %d, want none after refused calls", n)
	}
}

// addItemFor adds an item for another owner.
func (h *harness) addItemFor(t *testing.T, owner, title string) *dojov1.Item {
	t.Helper()
	res, err := h.client.AddItem(h.ctxFor(t, owner), &dojov1.AddItemRequest{Title: title, Kind: dojov1.ItemKind_ITEM_KIND_SKILL})
	if err != nil {
		t.Fatalf("add item for %s: %v", owner, err)
	}
	return res.GetItem()
}

func TestGetActivityReturnsItsItem(t *testing.T) {
	h := newHarness(t)
	item := h.addItem(t, "go")
	withItem := h.logActivity(t, item.GetId(), "chapter 1")
	loose := h.logActivity(t, "", "podcast")

	a, aErr := h.client.GetActivity(h.ctx(t), &dojov1.GetActivityRequest{Id: withItem.GetId()})
	b, bErr := h.client.GetActivity(h.ctx(t), &dojov1.GetActivityRequest{Id: loose.GetId()})
	_, otherErr := h.client.GetActivity(h.ctxFor(t, uuid.NewString()), &dojov1.GetActivityRequest{Id: withItem.GetId()})

	if aErr != nil || a.GetItem().GetId() != item.GetId() || a.GetActivity().GetSummary() != "chapter 1" {
		t.Fatalf("with item = %v, %v", a, aErr)
	}
	if bErr != nil || b.GetItem() != nil {
		t.Fatalf("loose = %v, %v; want no item", b, bErr)
	}
	requireStatus(t, otherErr, codes.NotFound, "ACTIVITY_NOT_FOUND")
}

func TestListActivitiesFiltersByItemAndPages(t *testing.T) {
	h := newHarness(t)
	item := h.addItem(t, "go")
	h.logActivity(t, item.GetId(), "one")
	h.logActivity(t, "", "two")
	h.logActivity(t, item.GetId(), "three")

	all, allErr := h.client.ListActivities(h.ctx(t), &dojov1.ListActivitiesRequest{})
	forItem, itemErr := h.client.ListActivities(h.ctx(t), &dojov1.ListActivitiesRequest{ItemId: item.GetId()})
	first, firstErr := h.client.ListActivities(h.ctx(t), &dojov1.ListActivitiesRequest{PageSize: 2})
	second, secondErr := h.client.ListActivities(h.ctx(t), &dojov1.ListActivitiesRequest{PageSize: 2, PageToken: first.GetNextPageToken()})
	_, idErr := h.client.ListActivities(h.ctx(t), &dojov1.ListActivitiesRequest{ItemId: "nope"})

	if allErr != nil || len(all.GetActivities()) != 3 || all.GetActivities()[0].GetSummary() != "three" {
		t.Fatalf("all = %v, %v", all, allErr)
	}
	if itemErr != nil || len(forItem.GetActivities()) != 2 {
		t.Fatalf("for item = %v, %v", forItem, itemErr)
	}
	if firstErr != nil || secondErr != nil || len(first.GetActivities()) != 2 || len(second.GetActivities()) != 1 || second.GetNextPageToken() != "" {
		t.Fatalf("pages = %d then %d, %v %v", len(first.GetActivities()), len(second.GetActivities()), firstErr, secondErr)
	}
	requireStatus(t, idErr, codes.InvalidArgument, "INVALID_ID")
}

func TestGeneratePostAsksFudeForAPostAboutTheActivities(t *testing.T) {
	h := newHarness(t)
	item := h.addItem(t, "Go course")
	first := h.logActivity(t, item.GetId(), "built a worker pool")
	second := h.logActivity(t, "", "read the race detector docs")

	res, err := h.client.GeneratePost(h.ctx(t), &dojov1.GeneratePostRequest{
		ActivityIds: []string{second.GetId(), first.GetId(), first.GetId()}, Channel: fudev1.Channel_CHANNEL_LINKEDIN,
	})

	if err != nil || res.GetDraftId() != h.fude.draftID {
		t.Fatalf("draft id = %q, %v; want %q", res.GetDraftId(), err, h.fude.draftID)
	}
	calls := h.fude.requests()
	if len(calls) != 1 {
		t.Fatalf("fude calls = %d, want 1", len(calls))
	}
	call := calls[0]
	wantTarget := min(first.GetId(), second.GetId())
	if call.GetKind() != fudev1.DraftKind_DRAFT_KIND_POST || call.GetTargetType() != fudev1.TargetType_TARGET_TYPE_LEARNING_ACTIVITY ||
		call.GetChannel() != fudev1.Channel_CHANNEL_LINKEDIN || call.GetTargetId() != wantTarget {
		t.Fatalf("request = %+v, want a linkedin post about activity %s", call, wantTarget)
	}
	for _, want := range []string{"built a worker pool", "read the race detector docs", "[Go course]", "worth it", "2026-10-07"} {
		if !strings.Contains(call.GetExtraContext(), want) {
			t.Errorf("extra context lacks %q:\n%s", want, call.GetExtraContext())
		}
	}
	if n := strings.Count(call.GetExtraContext(), "built a worker pool"); n != 1 {
		t.Errorf("a duplicated id listed the activity %d times, want once", n)
	}
}

func TestGeneratePostRefusesBadRequests(t *testing.T) {
	h := newHarness(t)
	own := h.logActivity(t, "", "mine")
	other := h.logActivityFor(t, uuid.NewString(), "theirs")
	many := make([]string, 11)
	for i := range many {
		many[i] = uuid.NewString()
	}
	tests := []struct {
		name   string
		req    *dojov1.GeneratePostRequest
		code   codes.Code
		reason string
	}{
		{"no activities", &dojov1.GeneratePostRequest{Channel: fudev1.Channel_CHANNEL_X}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"too many", &dojov1.GeneratePostRequest{ActivityIds: many, Channel: fudev1.Channel_CHANNEL_X}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"email is not a post channel", &dojov1.GeneratePostRequest{ActivityIds: []string{own.GetId()}, Channel: fudev1.Channel_CHANNEL_EMAIL}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"no channel", &dojov1.GeneratePostRequest{ActivityIds: []string{own.GetId()}}, codes.InvalidArgument, "INVALID_ARGUMENT"},
		{"bad id", &dojov1.GeneratePostRequest{ActivityIds: []string{"nope"}, Channel: fudev1.Channel_CHANNEL_X}, codes.InvalidArgument, "INVALID_ID"},
		{"another owner's activity", &dojov1.GeneratePostRequest{ActivityIds: []string{other.GetId()}, Channel: fudev1.Channel_CHANNEL_X}, codes.NotFound, "ACTIVITY_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.GeneratePost(h.ctx(t), tt.req)

			requireStatus(t, err, tt.code, tt.reason)
		})
	}
	if n := len(h.fude.requests()); n != 0 {
		t.Fatalf("fude calls = %d, want none for refused requests", n)
	}
}

func TestGeneratePostReportsWhenFudeIsDown(t *testing.T) {
	h := newHarness(t)
	activity := h.logActivity(t, "", "mine")
	h.fude.err = errFudeDown

	_, err := h.client.GeneratePost(h.ctx(t), &dojov1.GeneratePostRequest{
		ActivityIds: []string{activity.GetId()}, Channel: fudev1.Channel_CHANNEL_X,
	})

	requireStatus(t, err, codes.Unavailable, "DRAFTS_UNAVAILABLE")
	if strings.Contains(err.Error(), errFudeDown.Error()) {
		t.Fatalf("error %q leaks the downstream cause", err)
	}
}

// logActivityFor logs an activity for another owner.
func (h *harness) logActivityFor(t *testing.T, owner, summary string) *dojov1.Activity {
	t.Helper()
	res, err := h.client.LogActivity(h.ctxFor(t, owner), &dojov1.LogActivityRequest{Summary: summary})
	if err != nil {
		t.Fatalf("log activity for %s: %v", owner, err)
	}
	return res.GetActivity()
}
