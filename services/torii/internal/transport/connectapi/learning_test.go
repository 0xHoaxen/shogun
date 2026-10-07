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
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

// fakeDojo records the last request of each RPC and answers with err, or with
// the canned item, activity and draft id.
type fakeDojo struct {
	err error

	listItems   *dojov1.ListItemsRequest
	addItem     *dojov1.AddItemRequest
	updateItem  *dojov1.UpdateItemRequest
	changeItem  *dojov1.ChangeItemStatusRequest
	listActs    *dojov1.ListActivitiesRequest
	logActivity *dojov1.LogActivityRequest
	post        *dojov1.GeneratePostRequest
}

var cannedItem = &dojov1.Item{
	Id: "item-1", Title: "Go course", Kind: dojov1.ItemKind_ITEM_KIND_COURSE, Url: "https://example.com",
	Status: dojov1.ItemStatus_ITEM_STATUS_IN_PROGRESS, StartedOn: "2026-10-01", Insight: "channels", Version: 3,
}

var cannedActivity = &dojov1.Activity{
	Id: "act-1", ItemId: "item-1", Summary: "built a pool", Minutes: 45, OccurredOn: "2026-10-07", Tags: []string{"go"},
}

func (f *fakeDojo) ListItems(_ context.Context, in *dojov1.ListItemsRequest, _ ...grpc.CallOption) (*dojov1.ListItemsResponse, error) {
	f.listItems = in
	return &dojov1.ListItemsResponse{Items: []*dojov1.Item{cannedItem}, NextPageToken: "next"}, f.err
}

func (f *fakeDojo) AddItem(_ context.Context, in *dojov1.AddItemRequest, _ ...grpc.CallOption) (*dojov1.AddItemResponse, error) {
	f.addItem = in
	return &dojov1.AddItemResponse{Item: cannedItem}, f.err
}

func (f *fakeDojo) UpdateItem(_ context.Context, in *dojov1.UpdateItemRequest, _ ...grpc.CallOption) (*dojov1.UpdateItemResponse, error) {
	f.updateItem = in
	return &dojov1.UpdateItemResponse{Item: cannedItem}, f.err
}

func (f *fakeDojo) ChangeItemStatus(_ context.Context, in *dojov1.ChangeItemStatusRequest, _ ...grpc.CallOption) (*dojov1.ChangeItemStatusResponse, error) {
	f.changeItem = in
	return &dojov1.ChangeItemStatusResponse{Item: cannedItem}, f.err
}

func (f *fakeDojo) ListActivities(_ context.Context, in *dojov1.ListActivitiesRequest, _ ...grpc.CallOption) (*dojov1.ListActivitiesResponse, error) {
	f.listActs = in
	return &dojov1.ListActivitiesResponse{Activities: []*dojov1.Activity{cannedActivity}, NextPageToken: "more"}, f.err
}

func (f *fakeDojo) LogActivity(_ context.Context, in *dojov1.LogActivityRequest, _ ...grpc.CallOption) (*dojov1.LogActivityResponse, error) {
	f.logActivity = in
	return &dojov1.LogActivityResponse{Activity: cannedActivity}, f.err
}

func (f *fakeDojo) GeneratePost(_ context.Context, in *dojov1.GeneratePostRequest, _ ...grpc.CallOption) (*dojov1.GeneratePostResponse, error) {
	f.post = in
	return &dojov1.GeneratePostResponse{DraftId: "draft-1"}, f.err
}

func newLearningHarness(t *testing.T) (*fakeDojo, apiv1connect.LearningServiceClient, string) {
	t.Helper()
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
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
	fake := &fakeDojo{}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewLearningServiceHandler(connectapi.NewLearningServer(fake, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fake, apiv1connect.NewLearningServiceClient(srv.Client(), srv.URL), token
}

func TestListLearningItemsMapsTheFilterAndTheItems(t *testing.T) {
	fake, client, token := newLearningHarness(t)

	resp, err := client.ListLearningItems(context.Background(), withCookie(connect.NewRequest(&apiv1.ListLearningItemsRequest{
		Status: apiv1.ItemStatus_ITEM_STATUS_IN_PROGRESS, PageSize: 20, PageToken: "tok",
	}), token))
	if err != nil {
		t.Fatalf("ListLearningItems: %v", err)
	}
	if fake.listItems.GetStatus() != dojov1.ItemStatus_ITEM_STATUS_IN_PROGRESS || fake.listItems.GetPageSize() != 20 || fake.listItems.GetPageToken() != "tok" {
		t.Fatalf("dojo got %+v", fake.listItems)
	}
	item := resp.Msg.GetItems()[0]
	if item.GetId() != "item-1" || item.GetKind() != apiv1.ItemKind_ITEM_KIND_COURSE || item.GetStatus() != apiv1.ItemStatus_ITEM_STATUS_IN_PROGRESS ||
		item.GetVersion() != 3 || item.GetStartedOn() != "2026-10-01" || resp.Msg.GetNextPageToken() != "next" {
		t.Fatalf("got %+v", resp.Msg)
	}
}

func TestAddLearningItemPassesTheFieldsAndReturnsTheItem(t *testing.T) {
	fake, client, token := newLearningHarness(t)

	resp, err := client.AddLearningItem(context.Background(), withCookie(connect.NewRequest(&apiv1.AddLearningItemRequest{
		Title: "Go course", Kind: apiv1.ItemKind_ITEM_KIND_BOOK, Url: "https://example.com", Insight: "x",
	}), token))

	if err != nil || resp.Msg.GetItem().GetId() != "item-1" {
		t.Fatalf("got %v, %v", resp.Msg, err)
	}
	if fake.addItem.GetTitle() != "Go course" || fake.addItem.GetKind() != dojov1.ItemKind_ITEM_KIND_BOOK ||
		fake.addItem.GetUrl() != "https://example.com" || fake.addItem.GetInsight() != "x" {
		t.Fatalf("dojo got %+v", fake.addItem)
	}
}

func TestUpdateLearningItemPassesTheMaskAndTheVersion(t *testing.T) {
	fake, client, token := newLearningHarness(t)

	_, err := client.UpdateLearningItem(context.Background(), withCookie(connect.NewRequest(&apiv1.UpdateLearningItemRequest{
		Item:       &apiv1.LearningItem{Id: "item-1", Title: "new", Version: 3},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	}), token))
	if err != nil {
		t.Fatalf("UpdateLearningItem: %v", err)
	}
	got := fake.updateItem
	if got.GetItem().GetId() != "item-1" || got.GetItem().GetTitle() != "new" || got.GetItem().GetVersion() != 3 ||
		len(got.GetUpdateMask().GetPaths()) != 1 || got.GetUpdateMask().GetPaths()[0] != "title" {
		t.Fatalf("dojo got %+v", got)
	}
}

func TestChangeLearningItemStatusPassesTheMove(t *testing.T) {
	fake, client, token := newLearningHarness(t)

	_, err := client.ChangeLearningItemStatus(context.Background(), withCookie(connect.NewRequest(&apiv1.ChangeLearningItemStatusRequest{
		Id: "item-1", ToStatus: apiv1.ItemStatus_ITEM_STATUS_DONE, Version: 3,
	}), token))

	if err != nil || fake.changeItem.GetId() != "item-1" || fake.changeItem.GetToStatus() != dojov1.ItemStatus_ITEM_STATUS_DONE || fake.changeItem.GetVersion() != 3 {
		t.Fatalf("err %v, dojo got %+v", err, fake.changeItem)
	}
}

func TestListLearningActivitiesFiltersByItem(t *testing.T) {
	fake, client, token := newLearningHarness(t)

	resp, err := client.ListLearningActivities(context.Background(), withCookie(connect.NewRequest(&apiv1.ListLearningActivitiesRequest{
		ItemId: "item-1", PageSize: 5,
	}), token))

	if err != nil || fake.listActs.GetItemId() != "item-1" || fake.listActs.GetPageSize() != 5 {
		t.Fatalf("err %v, dojo got %+v", err, fake.listActs)
	}
	a := resp.Msg.GetActivities()[0]
	if a.GetSummary() != "built a pool" || a.GetMinutes() != 45 || a.GetOccurredOn() != "2026-10-07" || a.GetTags()[0] != "go" || resp.Msg.GetNextPageToken() != "more" {
		t.Fatalf("got %+v", resp.Msg)
	}
}

func TestLogLearningActivityPassesTheFields(t *testing.T) {
	fake, client, token := newLearningHarness(t)

	resp, err := client.LogLearningActivity(context.Background(), withCookie(connect.NewRequest(&apiv1.LogLearningActivityRequest{
		ItemId: "item-1", Summary: "built a pool", Minutes: 45, OccurredOn: "2026-10-07", Tags: []string{"go"},
	}), token))

	got := fake.logActivity
	if err != nil || resp.Msg.GetActivity().GetId() != "act-1" {
		t.Fatalf("got %v, %v", resp.Msg, err)
	}
	if got.GetItemId() != "item-1" || got.GetSummary() != "built a pool" || got.GetMinutes() != 45 || got.GetOccurredOn() != "2026-10-07" || got.GetTags()[0] != "go" {
		t.Fatalf("dojo got %+v", got)
	}
}

func TestGenerateLearningPostMapsTheChannelAndReturnsTheDraftID(t *testing.T) {
	fake, client, token := newLearningHarness(t)

	resp, err := client.GenerateLearningPost(context.Background(), withCookie(connect.NewRequest(&apiv1.GenerateLearningPostRequest{
		ActivityIds: []string{"act-1", "act-2"}, Channel: apiv1.DraftChannel_DRAFT_CHANNEL_X,
	}), token))

	if err != nil || resp.Msg.GetDraftId() != "draft-1" {
		t.Fatalf("got %v, %v", resp.Msg, err)
	}
	if len(fake.post.GetActivityIds()) != 2 || fake.post.GetChannel() != fudev1.Channel_CHANNEL_X {
		t.Fatalf("dojo got %+v", fake.post)
	}
}

func TestLearningErrorsKeepTheirReasonsAndHideInternals(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   connect.Code
		wantReason string
	}{
		{"stale version", withInfo(codes.Aborted, "VERSION_CONFLICT"), connect.CodeAborted, "VERSION_CONFLICT"},
		{"invalid move", withInfo(codes.FailedPrecondition, "ITEM_STATUS_INVALID_TRANSITION"), connect.CodeFailedPrecondition, "ITEM_STATUS_INVALID_TRANSITION"},
		{"missing item", withInfo(codes.NotFound, "ITEM_NOT_FOUND"), connect.CodeNotFound, "ITEM_NOT_FOUND"},
		{"bad input", withInfo(codes.InvalidArgument, "INVALID_ARGUMENT"), connect.CodeInvalidArgument, "INVALID_ARGUMENT"},
		{"dojo down", withInfo(codes.Unavailable, "DRAFTS_UNAVAILABLE"), connect.CodeUnavailable, "UNAVAILABLE"},
		{"internals", status.Error(codes.Internal, "secret internals"), connect.CodeInternal, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, client, token := newLearningHarness(t)
			fake.err = tt.err

			_, err := client.ChangeLearningItemStatus(context.Background(), withCookie(connect.NewRequest(&apiv1.ChangeLearningItemStatusRequest{Id: "item-1"}), token))

			code, reason := codeAndReason(t, err)
			if code != tt.wantCode || reason != tt.wantReason {
				t.Fatalf("got %s %q, want %s %q", code, reason, tt.wantCode, tt.wantReason)
			}
			if err != nil && strings.Contains(err.Error(), "secret internals") {
				t.Fatalf("internals leaked: %v", err)
			}
		})
	}
}

func TestLearningCallsRequireASession(t *testing.T) {
	fake, client, _ := newLearningHarness(t)

	_, err := client.ListLearningItems(context.Background(), connect.NewRequest(&apiv1.ListLearningItemsRequest{}))

	if code, _ := codeAndReason(t, err); code != connect.CodeUnauthenticated {
		t.Fatalf("got %v", err)
	}
	if fake.listItems != nil {
		t.Fatal("dojo was called without a session")
	}
}
