package grpc

import (
	"context"

	"github.com/google/uuid"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store"
)

// LogActivity implements dojo.v1.DojoService.
func (s *Server) LogActivity(ctx context.Context, req *dojov1.LogActivityRequest) (*dojov1.LogActivityResponse, error) {
	itemID, err := parseOptionalID("item_id", req.GetItemId())
	if err != nil {
		return nil, toStatus(err)
	}
	on, err := parseDate("occurred_on", req.GetOccurredOn())
	if err != nil {
		return nil, toStatus(err)
	}
	row, err := s.svc.LogActivity(ctx, itemID, domain.ActivityInput{
		Summary: req.GetSummary(), Minutes: req.GetMinutes(), OccurredOn: on, Tags: req.GetTags(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &dojov1.LogActivityResponse{Activity: activityToProto(row)}, nil
}

// GetActivity implements dojo.v1.DojoService.
func (s *Server) GetActivity(ctx context.Context, req *dojov1.GetActivityRequest) (*dojov1.GetActivityResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	detail, err := s.svc.GetActivity(ctx, id)
	if err != nil {
		return nil, toStatus(err)
	}
	res := &dojov1.GetActivityResponse{Activity: activityToProto(detail.Activity)}
	if detail.Item != nil {
		res.Item = itemToProto(*detail.Item)
	}
	return res, nil
}

// ListActivities implements dojo.v1.DojoService.
func (s *Server) ListActivities(ctx context.Context, req *dojov1.ListActivitiesRequest) (*dojov1.ListActivitiesResponse, error) {
	itemID, err := parseOptionalID("item_id", req.GetItemId())
	if err != nil {
		return nil, toStatus(err)
	}
	page, err := s.svc.ListActivities(ctx, itemID, store.Page{Size: req.GetPageSize(), Token: req.GetPageToken()})
	if err != nil {
		return nil, toStatus(err)
	}
	return &dojov1.ListActivitiesResponse{
		Activities: activitiesToProto(page.Activities), NextPageToken: page.NextPageToken,
	}, nil
}

// GeneratePost implements dojo.v1.DojoService.
func (s *Server) GeneratePost(ctx context.Context, req *dojov1.GeneratePostRequest) (*dojov1.GeneratePostResponse, error) {
	ids := make([]uuid.UUID, 0, len(req.GetActivityIds()))
	for _, raw := range req.GetActivityIds() {
		id, err := parseID("activity_ids", raw)
		if err != nil {
			return nil, toStatus(err)
		}
		ids = append(ids, id)
	}
	draftID, err := s.svc.GeneratePost(ctx, ids, req.GetChannel())
	if err != nil {
		return nil, toStatus(err)
	}
	return &dojov1.GeneratePostResponse{DraftId: draftID}, nil
}
