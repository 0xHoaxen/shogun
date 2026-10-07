package grpc_test

import (
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
)

func TestAddItemStoresAPlannedItemAndWritesNoEvent(t *testing.T) {
	h := newHarness(t)

	res, err := h.client.AddItem(h.ctx(t), &dojov1.AddItemRequest{
		Title: " Go in Practice ", Kind: dojov1.ItemKind_ITEM_KIND_BOOK, Url: "https://example.com/go", Insight: "idioms",
	})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	item := res.GetItem()
	if item.GetTitle() != "Go in Practice" || item.GetKind() != dojov1.ItemKind_ITEM_KIND_BOOK ||
		item.GetStatus() != dojov1.ItemStatus_ITEM_STATUS_PLANNED || item.GetVersion() != 1 || item.GetUrl() != "https://example.com/go" {
		t.Fatalf("item = %+v", item)
	}
	if n := h.outboxCount(t, ""); n != 0 {
		t.Fatalf("outbox rows = %d, want 0: nothing consumes an added item", n)
	}
}

func TestAddItemRefusesBadInput(t *testing.T) {
	tests := []struct {
		name string
		req  *dojov1.AddItemRequest
	}{
		{"no title", &dojov1.AddItemRequest{Kind: dojov1.ItemKind_ITEM_KIND_BOOK}},
		{"no kind", &dojov1.AddItemRequest{Title: "x"}},
		{"bad url", &dojov1.AddItemRequest{Title: "x", Kind: dojov1.ItemKind_ITEM_KIND_BOOK, Url: "javascript:alert(1)"}},
	}
	h := newHarness(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.AddItem(h.ctx(t), tt.req)

			requireStatus(t, err, codes.InvalidArgument, "INVALID_ARGUMENT")
		})
	}
}

func TestCallsWithoutAnIdentityAreRefused(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.ListItems(t.Context(), &dojov1.ListItemsRequest{})

	if got := codes.Unauthenticated; err == nil || !hasCode(err, got) {
		t.Fatalf("err = %v, want %s", err, got)
	}
}

func TestGetItemIsPerOwner(t *testing.T) {
	h := newHarness(t)
	item := h.addItem(t, "go")

	own, ownErr := h.client.GetItem(h.ctx(t), &dojov1.GetItemRequest{Id: item.GetId()})
	_, otherErr := h.client.GetItem(h.ctxFor(t, uuid.NewString()), &dojov1.GetItemRequest{Id: item.GetId()})
	_, badErr := h.client.GetItem(h.ctx(t), &dojov1.GetItemRequest{Id: "nope"})

	if ownErr != nil || own.GetItem().GetId() != item.GetId() {
		t.Fatalf("own get = %v, %v", own, ownErr)
	}
	requireStatus(t, otherErr, codes.NotFound, "ITEM_NOT_FOUND")
	requireStatus(t, badErr, codes.InvalidArgument, "INVALID_ID")
}

func TestListItemsPagesAndFiltersByStatus(t *testing.T) {
	h := newHarness(t)
	for _, title := range []string{"a", "b", "c"} {
		h.addItem(t, title)
	}
	started := h.addItem(t, "started")
	if _, err := h.client.ChangeItemStatus(h.ctx(t), &dojov1.ChangeItemStatusRequest{
		Id: started.GetId(), ToStatus: dojov1.ItemStatus_ITEM_STATUS_IN_PROGRESS, Version: 1,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	first, firstErr := h.client.ListItems(h.ctx(t), &dojov1.ListItemsRequest{PageSize: 3})
	second, secondErr := h.client.ListItems(h.ctx(t), &dojov1.ListItemsRequest{PageSize: 3, PageToken: first.GetNextPageToken()})
	filtered, filterErr := h.client.ListItems(h.ctx(t), &dojov1.ListItemsRequest{Status: dojov1.ItemStatus_ITEM_STATUS_IN_PROGRESS})
	_, tokenErr := h.client.ListItems(h.ctx(t), &dojov1.ListItemsRequest{PageToken: "!!"})

	if firstErr != nil || secondErr != nil || len(first.GetItems()) != 3 || len(second.GetItems()) != 1 || second.GetNextPageToken() != "" {
		t.Fatalf("pages = %d then %d (next %q), %v %v", len(first.GetItems()), len(second.GetItems()), second.GetNextPageToken(), firstErr, secondErr)
	}
	if first.GetItems()[0].GetTitle() != "started" {
		t.Fatalf("newest first: got %q", first.GetItems()[0].GetTitle())
	}
	if filterErr != nil || len(filtered.GetItems()) != 1 || filtered.GetItems()[0].GetId() != started.GetId() {
		t.Fatalf("filtered = %v, %v", filtered, filterErr)
	}
	requireStatus(t, tokenErr, codes.InvalidArgument, "INVALID_PAGE_TOKEN")
}

func TestUpdateItemAppliesOnlyTheMaskedFields(t *testing.T) {
	h := newHarness(t)
	item := h.addItem(t, "go")

	res, err := h.client.UpdateItem(h.ctx(t), &dojov1.UpdateItemRequest{
		Item:       &dojov1.Item{Id: item.GetId(), Version: 1, Title: "go, second edition", Insight: "ignored"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	got := res.GetItem()
	if got.GetTitle() != "go, second edition" || got.GetInsight() != "worth it" || got.GetVersion() != 2 {
		t.Fatalf("item = %+v, want the title changed, the insight kept and version 2", got)
	}
	if n := h.outboxCount(t, ""); n != 0 {
		t.Fatalf("outbox rows = %d, want 0", n)
	}
}

func TestUpdateItemRefusesStaleVersionsAndBadMasks(t *testing.T) {
	h := newHarness(t)
	item := h.addItem(t, "go")
	update := func(version int32, paths ...string) error {
		_, err := h.client.UpdateItem(h.ctx(t), &dojov1.UpdateItemRequest{
			Item:       &dojov1.Item{Id: item.GetId(), Version: version, Title: "new"},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
		})
		return err
	}

	stale := update(7, "title")
	empty := update(1)
	unknown := update(1, "status")
	blank := update(1, "kind")

	requireStatus(t, stale, codes.Aborted, "VERSION_CONFLICT")
	requireStatus(t, empty, codes.InvalidArgument, "INVALID_UPDATE_MASK")
	requireStatus(t, unknown, codes.InvalidArgument, "INVALID_UPDATE_MASK")
	requireStatus(t, blank, codes.InvalidArgument, "INVALID_ARGUMENT") // the kind in the request is unspecified
}

func TestChangeItemStatusToDoneWritesOneCompletedEvent(t *testing.T) {
	h := newHarness(t)
	item := h.addItem(t, "go")
	started, err := h.client.ChangeItemStatus(h.ctx(t), &dojov1.ChangeItemStatusRequest{
		Id: item.GetId(), ToStatus: dojov1.ItemStatus_ITEM_STATUS_IN_PROGRESS, Version: 1,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	afterStart := h.outboxCount(t, "")

	done, err := h.client.ChangeItemStatus(h.ctx(t), &dojov1.ChangeItemStatusRequest{
		Id: item.GetId(), ToStatus: dojov1.ItemStatus_ITEM_STATUS_DONE, Version: started.GetItem().GetVersion(),
	})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if started.GetItem().GetStartedOn() != "2026-10-07" || done.GetItem().GetCompletedOn() != "2026-10-07" {
		t.Fatalf("dates: started %q, completed %q, want today's IST date", started.GetItem().GetStartedOn(), done.GetItem().GetCompletedOn())
	}
	if afterStart != 0 || h.outboxCount(t, "learning.item_completed") != 1 || h.outboxCount(t, "") != 1 {
		t.Fatalf("outbox rows after start %d, completed %d, total %d; want 0, 1, 1", afterStart, h.outboxCount(t, "learning.item_completed"), h.outboxCount(t, ""))
	}
	var payload dojov1.LearningItemCompleted
	h.payloadOf(t, "learning.item_completed", &payload)
	if payload.GetItemId() != item.GetId() || payload.GetTitle() != "go" || payload.GetOwnerId() != h.owner ||
		payload.GetKind() != dojov1.ItemKind_ITEM_KIND_COURSE {
		t.Fatalf("payload = %+v", &payload)
	}
}

func TestChangeItemStatusRefusesInvalidMovesAndStaleVersions(t *testing.T) {
	h := newHarness(t)
	item := h.addItem(t, "go")
	move := func(to dojov1.ItemStatus, version int32) error {
		_, err := h.client.ChangeItemStatus(h.ctx(t), &dojov1.ChangeItemStatusRequest{Id: item.GetId(), ToStatus: to, Version: version})
		return err
	}

	samePlace := move(dojov1.ItemStatus_ITEM_STATUS_PLANNED, 1)
	stale := move(dojov1.ItemStatus_ITEM_STATUS_DONE, 9)
	unknown := move(dojov1.ItemStatus_ITEM_STATUS_UNSPECIFIED, 1)
	missing := func() error {
		_, err := h.client.ChangeItemStatus(h.ctx(t), &dojov1.ChangeItemStatusRequest{
			Id: uuid.NewString(), ToStatus: dojov1.ItemStatus_ITEM_STATUS_DONE, Version: 1,
		})
		return err
	}()

	requireStatus(t, samePlace, codes.FailedPrecondition, "ITEM_STATUS_INVALID_TRANSITION")
	requireStatus(t, stale, codes.Aborted, "VERSION_CONFLICT")
	requireStatus(t, unknown, codes.InvalidArgument, "INVALID_ARGUMENT")
	requireStatus(t, missing, codes.NotFound, "ITEM_NOT_FOUND")
	if n := h.outboxCount(t, ""); n != 0 {
		t.Fatalf("outbox rows = %d, want none after refused moves", n)
	}
}
