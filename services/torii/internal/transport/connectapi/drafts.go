package connectapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
)

// Reasons fude gives an approval whose send did not clearly succeed or fail.
// They are the only Unavailable answers that reach the browser with their own
// reason, because the owner must be told whether the draft is back in the queue
// or the outcome is not yet known.
const (
	reasonSendUnavailable = "SEND_UNAVAILABLE"
	reasonSendUnknown     = "SEND_STATUS_UNKNOWN"
)

// DraftsBackend is the part of fude's client DraftsServer uses.
type DraftsBackend interface {
	ListQueue(ctx context.Context, in *fudev1.ListQueueRequest, opts ...grpc.CallOption) (*fudev1.ListQueueResponse, error)
	GetDraft(ctx context.Context, in *fudev1.GetDraftRequest, opts ...grpc.CallOption) (*fudev1.GetDraftResponse, error)
	GenerateDraft(ctx context.Context, in *fudev1.GenerateDraftRequest, opts ...grpc.CallOption) (*fudev1.GenerateDraftResponse, error)
	Regenerate(ctx context.Context, in *fudev1.RegenerateRequest, opts ...grpc.CallOption) (*fudev1.RegenerateResponse, error)
	EditDraft(ctx context.Context, in *fudev1.EditDraftRequest, opts ...grpc.CallOption) (*fudev1.EditDraftResponse, error)
	Approve(ctx context.Context, in *fudev1.ApproveRequest, opts ...grpc.CallOption) (*fudev1.ApproveResponse, error)
	Discard(ctx context.Context, in *fudev1.DiscardRequest, opts ...grpc.CallOption) (*fudev1.DiscardResponse, error)
	MarkPosted(ctx context.Context, in *fudev1.MarkPostedRequest, opts ...grpc.CallOption) (*fudev1.MarkPostedResponse, error)
}

// DraftsServer implements shogun.api.v1.DraftsService on top of fude.
type DraftsServer struct {
	fude DraftsBackend
	log  *slog.Logger
}

var _ apiv1connect.DraftsServiceHandler = (*DraftsServer)(nil)

// NewDraftsServer returns a DraftsServer that calls fude through backend.
func NewDraftsServer(backend DraftsBackend, log *slog.Logger) *DraftsServer {
	return &DraftsServer{fude: backend, log: log}
}

// ListQueue returns one page of drafts in a state.
func (s *DraftsServer) ListQueue(
	ctx context.Context, req *connect.Request[apiv1.ListQueueRequest],
) (*connect.Response[apiv1.ListQueueResponse], error) {
	in := req.Msg
	resp, err := s.fude.ListQueue(ctx, &fudev1.ListQueueRequest{
		State: draftStateToFude(in.GetState()), PageSize: in.GetPageSize(), PageToken: in.GetPageToken(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	drafts := make([]*apiv1.Draft, 0, len(resp.GetDrafts()))
	for _, d := range resp.GetDrafts() {
		drafts = append(drafts, draftToAPI(d))
	}
	return connect.NewResponse(&apiv1.ListQueueResponse{Drafts: drafts, NextPageToken: resp.GetNextPageToken()}), nil
}

// GetDraft returns a draft with every version.
func (s *DraftsServer) GetDraft(
	ctx context.Context, req *connect.Request[apiv1.GetDraftRequest],
) (*connect.Response[apiv1.GetDraftResponse], error) {
	resp, err := s.fude.GetDraft(ctx, &fudev1.GetDraftRequest{DraftId: req.Msg.GetId()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	versions := make([]*apiv1.DraftVersion, 0, len(resp.GetVersions()))
	for _, v := range resp.GetVersions() {
		versions = append(versions, draftVersionToAPI(v))
	}
	return connect.NewResponse(&apiv1.GetDraftResponse{Draft: draftToAPI(resp.GetDraft()), Versions: versions}), nil
}

// GenerateDraft queues a new draft; a retry with the same Idempotency-Key
// returns the original.
func (s *DraftsServer) GenerateDraft(
	ctx context.Context, req *connect.Request[apiv1.GenerateDraftRequest],
) (*connect.Response[apiv1.GenerateDraftResponse], error) {
	in := req.Msg
	resp, err := s.fude.GenerateDraft(ctx, &fudev1.GenerateDraftRequest{
		Kind: draftKindToFude(in.GetKind()), TargetType: draftTargetToFude(in.GetTarget()), TargetId: in.GetTargetId(),
		Channel: draftChannelToFude(in.GetChannel()), Recipient: in.GetRecipient(), ExtraContext: in.GetExtraContext(),
		IdempotencyKey: req.Header().Get(idempotencyKeyHeader),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.GenerateDraftResponse{Draft: draftToAPI(resp.GetDraft())}), nil
}

// Regenerate queues a new AI version of a pending draft.
func (s *DraftsServer) Regenerate(
	ctx context.Context, req *connect.Request[apiv1.RegenerateRequest],
) (*connect.Response[apiv1.RegenerateResponse], error) {
	in := req.Msg
	resp, err := s.fude.Regenerate(ctx, &fudev1.RegenerateRequest{DraftId: in.GetId(), ExtraContext: in.GetExtraContext(), Version: in.GetVersion()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.RegenerateResponse{Draft: draftToAPI(resp.GetDraft())}), nil
}

// EditDraft stores the owner's text as a new version.
func (s *DraftsServer) EditDraft(
	ctx context.Context, req *connect.Request[apiv1.EditDraftRequest],
) (*connect.Response[apiv1.EditDraftResponse], error) {
	in := req.Msg
	resp, err := s.fude.EditDraft(ctx, &fudev1.EditDraftRequest{DraftId: in.GetId(), Subject: in.GetSubject(), Body: in.GetBody(), Version: in.GetVersion()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.EditDraftResponse{
		Draft: draftToAPI(resp.GetDraft()), DraftVersion: draftVersionToAPI(resp.GetDraftVersion()),
	}), nil
}

// Approve approves one exact version; for email it is then sent.
func (s *DraftsServer) Approve(
	ctx context.Context, req *connect.Request[apiv1.ApproveRequest],
) (*connect.Response[apiv1.ApproveResponse], error) {
	in := req.Msg
	resp, err := s.fude.Approve(ctx, &fudev1.ApproveRequest{DraftId: in.GetId(), Version: in.GetVersion(), BodySha256: in.GetBodySha256()})
	if err != nil {
		return nil, s.approveError(ctx, err)
	}
	return connect.NewResponse(&apiv1.ApproveResponse{Draft: draftToAPI(resp.GetDraft()), CopyReady: resp.GetCopyReady()}), nil
}

// approveError is fromGRPC, except that the two answers that say whether the
// mail may have gone out keep their reason instead of becoming "try again".
func (s *DraftsServer) approveError(ctx context.Context, err error) error {
	if st, ok := status.FromError(err); ok {
		if reason := reasonOf(st); reason == reasonSendUnavailable || reason == reasonSendUnknown {
			return newError(connect.CodeUnavailable, reason, st.Message())
		}
	}
	return fromGRPC(ctx, s.log, err)
}

// Discard moves a pending draft to discarded.
func (s *DraftsServer) Discard(
	ctx context.Context, req *connect.Request[apiv1.DiscardRequest],
) (*connect.Response[apiv1.DiscardResponse], error) {
	in := req.Msg
	resp, err := s.fude.Discard(ctx, &fudev1.DiscardRequest{DraftId: in.GetId(), Version: in.GetVersion()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.DiscardResponse{Draft: draftToAPI(resp.GetDraft())}), nil
}

// MarkPosted records that the owner posted an approved copy-only draft.
func (s *DraftsServer) MarkPosted(
	ctx context.Context, req *connect.Request[apiv1.MarkPostedRequest],
) (*connect.Response[apiv1.MarkPostedResponse], error) {
	in := req.Msg
	resp, err := s.fude.MarkPosted(ctx, &fudev1.MarkPostedRequest{DraftId: in.GetId(), Version: in.GetVersion()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.MarkPostedResponse{Draft: draftToAPI(resp.GetDraft())}), nil
}
