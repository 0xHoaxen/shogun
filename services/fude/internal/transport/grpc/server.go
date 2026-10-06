// Package grpc holds the fude gRPC handlers. They convert requests to use case
// inputs, call internal/app, and map its errors to gRPC status codes.
package grpc

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/wire"
)

// Server implements fude.v1.FudeService. RPCs without a handler yet answer
// Unimplemented.
type Server struct {
	fudev1.UnimplementedFudeServiceServer
	svc      *app.Service
	approver *app.Approver
}

// New returns a Server that runs its calls on svc, and approvals on approver.
func New(svc *app.Service, approver *app.Approver) *Server {
	return &Server{svc: svc, approver: approver}
}

func badEnum(field string) error {
	return &app.InvalidArgumentError{Reason: "INVALID_" + field, Field: field, Msg: field + " is missing or unknown"}
}

// GenerateDraft implements fude.v1.FudeService.
func (s *Server) GenerateDraft(ctx context.Context, req *fudev1.GenerateDraftRequest) (*fudev1.GenerateDraftResponse, error) {
	kind, _ := wire.KindFromProto(req.GetKind())
	target, _ := wire.TargetTypeFromProto(req.GetTargetType())
	channel, _ := wire.ChannelFromProto(req.GetChannel())
	d, err := s.svc.GenerateDraft(ctx, app.GenerateDraftInput{
		Kind: kind, TargetType: target, TargetID: req.GetTargetId(), Channel: channel,
		ExtraContext: req.GetExtraContext(), Recipient: req.GetRecipient(), IdempotencyKey: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &fudev1.GenerateDraftResponse{Draft: draftToProto(d)}, nil
}

// Regenerate implements fude.v1.FudeService.
func (s *Server) Regenerate(ctx context.Context, req *fudev1.RegenerateRequest) (*fudev1.RegenerateResponse, error) {
	d, err := s.svc.Regenerate(ctx, req.GetDraftId(), req.GetExtraContext(), req.GetVersion())
	if err != nil {
		return nil, toStatus(err)
	}
	return &fudev1.RegenerateResponse{Draft: draftToProto(d)}, nil
}

// EditDraft implements fude.v1.FudeService.
func (s *Server) EditDraft(ctx context.Context, req *fudev1.EditDraftRequest) (*fudev1.EditDraftResponse, error) {
	d, v, err := s.svc.EditDraft(ctx, req.GetDraftId(), req.GetSubject(), req.GetBody(), req.GetVersion())
	if err != nil {
		return nil, toStatus(err)
	}
	return &fudev1.EditDraftResponse{Draft: draftToProto(d), DraftVersion: versionToProto(v)}, nil
}

// Discard implements fude.v1.FudeService.
func (s *Server) Discard(ctx context.Context, req *fudev1.DiscardRequest) (*fudev1.DiscardResponse, error) {
	d, err := s.svc.Discard(ctx, req.GetDraftId(), req.GetVersion())
	if err != nil {
		return nil, toStatus(err)
	}
	return &fudev1.DiscardResponse{Draft: draftToProto(d)}, nil
}

// GetDraft implements fude.v1.FudeService.
func (s *Server) GetDraft(ctx context.Context, req *fudev1.GetDraftRequest) (*fudev1.GetDraftResponse, error) {
	detail, err := s.svc.GetDraft(ctx, req.GetDraftId())
	if err != nil {
		return nil, toStatus(err)
	}
	versions := make([]*fudev1.DraftVersion, 0, len(detail.Versions))
	for _, v := range detail.Versions {
		versions = append(versions, versionToProto(v))
	}
	d := draftToProto(detail.Draft)
	for _, v := range detail.Versions {
		if v.Version == detail.Draft.CurrentVersion {
			d.Subject, d.Preview = deref(v.Subject), preview(v.Body)
		}
	}
	return &fudev1.GetDraftResponse{Draft: d, Versions: versions}, nil
}

// ListQueue implements fude.v1.FudeService.
func (s *Server) ListQueue(ctx context.Context, req *fudev1.ListQueueRequest) (*fudev1.ListQueueResponse, error) {
	var state domain.DraftState
	if req.GetState() != fudev1.DraftState_DRAFT_STATE_UNSPECIFIED {
		var ok bool
		if state, ok = wire.StateFromProto(req.GetState()); !ok {
			return nil, toStatus(badEnum("STATE"))
		}
	}
	res, err := s.svc.ListQueue(ctx, app.ListQueueInput{State: state, PageSize: req.GetPageSize(), PageToken: req.GetPageToken()})
	if err != nil {
		return nil, toStatus(err)
	}
	drafts := make([]*fudev1.Draft, 0, len(res.Drafts))
	for _, d := range res.Drafts {
		drafts = append(drafts, queueItemToProto(d))
	}
	return &fudev1.ListQueueResponse{Drafts: drafts, NextPageToken: res.NextPageToken}, nil
}

// AddVoiceSample implements fude.v1.FudeService.
func (s *Server) AddVoiceSample(ctx context.Context, req *fudev1.AddVoiceSampleRequest) (*fudev1.AddVoiceSampleResponse, error) {
	channel, _ := wire.ChannelFromProto(req.GetChannel())
	v, err := s.svc.AddVoiceSample(ctx, channel, req.GetText())
	if err != nil {
		return nil, toStatus(err)
	}
	return &fudev1.AddVoiceSampleResponse{Sample: &fudev1.VoiceSample{
		Id: v.ID.String(), Channel: req.GetChannel(), Text: v.Text, CreatedAt: timestamppb.New(v.CreatedAt),
	}}, nil
}

// Approve implements fude.v1.FudeService. The token it stamps stays inside the
// service; the response only says whether the owner must copy the text.
func (s *Server) Approve(ctx context.Context, req *fudev1.ApproveRequest) (*fudev1.ApproveResponse, error) {
	res, err := s.approver.Approve(ctx, app.ApproveInput{
		DraftID: req.GetDraftId(), Version: req.GetVersion(), BodySHA256: req.GetBodySha256(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &fudev1.ApproveResponse{Draft: draftToProto(res.Draft), CopyReady: res.CopyReady}, nil
}
