package grpc

import (
	"context"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	"github.com/0xHoaxen/shogun/services/dojo/internal/app"
	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store"
	"github.com/0xHoaxen/shogun/services/dojo/internal/wire"
)

// updatable are the item fields UpdateItem may name in its mask.
const (
	pathTitle   = "title"
	pathKind    = "kind"
	pathURL     = "url"
	pathInsight = "insight"
)

// Server implements dojo.v1.DojoService.
type Server struct {
	dojov1.UnimplementedDojoServiceServer
	svc *app.Service
}

// New returns a Server that runs its calls on svc.
func New(svc *app.Service) *Server { return &Server{svc: svc} }

// AddItem implements dojo.v1.DojoService.
func (s *Server) AddItem(ctx context.Context, req *dojov1.AddItemRequest) (*dojov1.AddItemResponse, error) {
	row, err := s.svc.AddItem(ctx, domain.ItemInput{
		Title: req.GetTitle(), Kind: wire.KindFromProto(req.GetKind()), URL: req.GetUrl(), Insight: req.GetInsight(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &dojov1.AddItemResponse{Item: itemToProto(row)}, nil
}

// GetItem implements dojo.v1.DojoService.
func (s *Server) GetItem(ctx context.Context, req *dojov1.GetItemRequest) (*dojov1.GetItemResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	row, err := s.svc.GetItem(ctx, id)
	if err != nil {
		return nil, toStatus(err)
	}
	return &dojov1.GetItemResponse{Item: itemToProto(row)}, nil
}

// ListItems implements dojo.v1.DojoService.
func (s *Server) ListItems(ctx context.Context, req *dojov1.ListItemsRequest) (*dojov1.ListItemsResponse, error) {
	var status *domain.ItemStatus
	if req.GetStatus() != dojov1.ItemStatus_ITEM_STATUS_UNSPECIFIED {
		st := wire.StatusFromProto(req.GetStatus())
		status = &st
	}
	page, err := s.svc.ListItems(ctx, status, store.Page{Size: req.GetPageSize(), Token: req.GetPageToken()})
	if err != nil {
		return nil, toStatus(err)
	}
	return &dojov1.ListItemsResponse{Items: itemsToProto(page.Items), NextPageToken: page.NextPageToken}, nil
}

// UpdateItem implements dojo.v1.DojoService.
func (s *Server) UpdateItem(ctx context.Context, req *dojov1.UpdateItemRequest) (*dojov1.UpdateItemResponse, error) {
	id, err := parseID("item.id", req.GetItem().GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	patch, err := patchFromMask(req.GetItem(), req.GetUpdateMask().GetPaths())
	if err != nil {
		return nil, toStatus(err)
	}
	row, err := s.svc.UpdateItem(ctx, id, patch, req.GetItem().GetVersion())
	if err != nil {
		return nil, toStatus(err)
	}
	return &dojov1.UpdateItemResponse{Item: itemToProto(row)}, nil
}

// ChangeItemStatus implements dojo.v1.DojoService.
func (s *Server) ChangeItemStatus(ctx context.Context, req *dojov1.ChangeItemStatusRequest) (*dojov1.ChangeItemStatusResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	row, err := s.svc.ChangeItemStatus(ctx, id, wire.StatusFromProto(req.GetToStatus()), req.GetVersion())
	if err != nil {
		return nil, toStatus(err)
	}
	return &dojov1.ChangeItemStatusResponse{Item: itemToProto(row)}, nil
}

// patchFromMask copies the masked fields of item into a patch. An empty mask or
// an unknown path is refused.
func patchFromMask(item *dojov1.Item, paths []string) (app.ItemPatch, error) {
	if len(paths) == 0 {
		return app.ItemPatch{}, &badRequest{reason: reasonInvalidMask, msg: "update_mask is empty"}
	}
	var patch app.ItemPatch
	for _, p := range paths {
		switch p {
		case pathTitle:
			patch.Title = ptr(item.GetTitle())
		case pathKind:
			patch.Kind = ptr(wire.KindFromProto(item.GetKind()))
		case pathURL:
			patch.URL = ptr(item.GetUrl())
		case pathInsight:
			patch.Insight = ptr(item.GetInsight())
		default:
			return app.ItemPatch{}, &badRequest{reason: reasonInvalidMask, msg: "update_mask path " + p + " is not editable"}
		}
	}
	return patch, nil
}

func ptr[T any](v T) *T { return &v }
