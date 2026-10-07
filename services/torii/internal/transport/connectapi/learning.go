package connectapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/grpc"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
)

// LearningBackend is the part of dojo's client LearningServer uses.
type LearningBackend interface {
	ListItems(ctx context.Context, in *dojov1.ListItemsRequest, opts ...grpc.CallOption) (*dojov1.ListItemsResponse, error)
	AddItem(ctx context.Context, in *dojov1.AddItemRequest, opts ...grpc.CallOption) (*dojov1.AddItemResponse, error)
	UpdateItem(ctx context.Context, in *dojov1.UpdateItemRequest, opts ...grpc.CallOption) (*dojov1.UpdateItemResponse, error)
	ChangeItemStatus(ctx context.Context, in *dojov1.ChangeItemStatusRequest, opts ...grpc.CallOption) (*dojov1.ChangeItemStatusResponse, error)
	ListActivities(ctx context.Context, in *dojov1.ListActivitiesRequest, opts ...grpc.CallOption) (*dojov1.ListActivitiesResponse, error)
	LogActivity(ctx context.Context, in *dojov1.LogActivityRequest, opts ...grpc.CallOption) (*dojov1.LogActivityResponse, error)
	GeneratePost(ctx context.Context, in *dojov1.GeneratePostRequest, opts ...grpc.CallOption) (*dojov1.GeneratePostResponse, error)
}

// LearningServer implements shogun.api.v1.LearningService on top of dojo.
type LearningServer struct {
	dojo LearningBackend
	log  *slog.Logger
}

var _ apiv1connect.LearningServiceHandler = (*LearningServer)(nil)

// NewLearningServer returns a LearningServer that calls dojo through backend.
func NewLearningServer(backend LearningBackend, log *slog.Logger) *LearningServer {
	return &LearningServer{dojo: backend, log: log}
}

// ListLearningItems returns one page of items, newest first.
func (s *LearningServer) ListLearningItems(
	ctx context.Context, req *connect.Request[apiv1.ListLearningItemsRequest],
) (*connect.Response[apiv1.ListLearningItemsResponse], error) {
	in := req.Msg
	resp, err := s.dojo.ListItems(ctx, &dojov1.ListItemsRequest{
		Status: itemStatusToDojo(in.GetStatus()), PageSize: in.GetPageSize(), PageToken: in.GetPageToken(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	items := make([]*apiv1.LearningItem, 0, len(resp.GetItems()))
	for _, i := range resp.GetItems() {
		items = append(items, learningItemToAPI(i))
	}
	return connect.NewResponse(&apiv1.ListLearningItemsResponse{Items: items, NextPageToken: resp.GetNextPageToken()}), nil
}

// AddLearningItem adds a planned item.
func (s *LearningServer) AddLearningItem(
	ctx context.Context, req *connect.Request[apiv1.AddLearningItemRequest],
) (*connect.Response[apiv1.AddLearningItemResponse], error) {
	in := req.Msg
	resp, err := s.dojo.AddItem(ctx, &dojov1.AddItemRequest{
		Title: in.GetTitle(), Kind: itemKindToDojo(in.GetKind()), Url: in.GetUrl(), Insight: in.GetInsight(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.AddLearningItemResponse{Item: learningItemToAPI(resp.GetItem())}), nil
}

// UpdateLearningItem changes the masked fields of an item.
func (s *LearningServer) UpdateLearningItem(
	ctx context.Context, req *connect.Request[apiv1.UpdateLearningItemRequest],
) (*connect.Response[apiv1.UpdateLearningItemResponse], error) {
	in := req.Msg
	resp, err := s.dojo.UpdateItem(ctx, &dojov1.UpdateItemRequest{
		Item: learningItemToDojo(in.GetItem()), UpdateMask: in.GetUpdateMask(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.UpdateLearningItemResponse{Item: learningItemToAPI(resp.GetItem())}), nil
}

// ChangeLearningItemStatus moves an item to another status.
func (s *LearningServer) ChangeLearningItemStatus(
	ctx context.Context, req *connect.Request[apiv1.ChangeLearningItemStatusRequest],
) (*connect.Response[apiv1.ChangeLearningItemStatusResponse], error) {
	in := req.Msg
	resp, err := s.dojo.ChangeItemStatus(ctx, &dojov1.ChangeItemStatusRequest{
		Id: in.GetId(), ToStatus: itemStatusToDojo(in.GetToStatus()), Version: in.GetVersion(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.ChangeLearningItemStatusResponse{Item: learningItemToAPI(resp.GetItem())}), nil
}

// ListLearningActivities returns one page of activities, newest first.
func (s *LearningServer) ListLearningActivities(
	ctx context.Context, req *connect.Request[apiv1.ListLearningActivitiesRequest],
) (*connect.Response[apiv1.ListLearningActivitiesResponse], error) {
	in := req.Msg
	resp, err := s.dojo.ListActivities(ctx, &dojov1.ListActivitiesRequest{
		ItemId: in.GetItemId(), PageSize: in.GetPageSize(), PageToken: in.GetPageToken(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	activities := make([]*apiv1.LearningActivity, 0, len(resp.GetActivities()))
	for _, a := range resp.GetActivities() {
		activities = append(activities, learningActivityToAPI(a))
	}
	return connect.NewResponse(&apiv1.ListLearningActivitiesResponse{
		Activities: activities, NextPageToken: resp.GetNextPageToken(),
	}), nil
}

// LogLearningActivity records an activity.
func (s *LearningServer) LogLearningActivity(
	ctx context.Context, req *connect.Request[apiv1.LogLearningActivityRequest],
) (*connect.Response[apiv1.LogLearningActivityResponse], error) {
	in := req.Msg
	resp, err := s.dojo.LogActivity(ctx, &dojov1.LogActivityRequest{
		ItemId: in.GetItemId(), Summary: in.GetSummary(), Minutes: in.GetMinutes(),
		OccurredOn: in.GetOccurredOn(), Tags: in.GetTags(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.LogLearningActivityResponse{Activity: learningActivityToAPI(resp.GetActivity())}), nil
}

// GenerateLearningPost asks for a post draft about activities.
func (s *LearningServer) GenerateLearningPost(
	ctx context.Context, req *connect.Request[apiv1.GenerateLearningPostRequest],
) (*connect.Response[apiv1.GenerateLearningPostResponse], error) {
	in := req.Msg
	resp, err := s.dojo.GeneratePost(ctx, &dojov1.GeneratePostRequest{
		ActivityIds: in.GetActivityIds(), Channel: draftChannelToFude(in.GetChannel()),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.GenerateLearningPostResponse{DraftId: resp.GetDraftId()}), nil
}
