package connectapi_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

// fakeFude answers from the funcs a test sets and records what it was asked.
type fakeFude struct {
	mu       sync.Mutex
	owners   []string
	requests []any

	list     func(*fudev1.ListQueueRequest) (*fudev1.ListQueueResponse, error)
	get      func(*fudev1.GetDraftRequest) (*fudev1.GetDraftResponse, error)
	generate func(*fudev1.GenerateDraftRequest) (*fudev1.GenerateDraftResponse, error)
	approve  func(*fudev1.ApproveRequest) (*fudev1.ApproveResponse, error)

	markPostedErr error
}

func (f *fakeFude) record(ctx context.Context, in any) {
	id, _ := authz.FromContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners = append(f.owners, id.OwnerID)
	f.requests = append(f.requests, in)
}

func (f *fakeFude) last() any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func (f *fakeFude) ListQueue(ctx context.Context, in *fudev1.ListQueueRequest, _ ...grpc.CallOption) (*fudev1.ListQueueResponse, error) {
	f.record(ctx, in)
	return f.list(in)
}

func (f *fakeFude) GetDraft(ctx context.Context, in *fudev1.GetDraftRequest, _ ...grpc.CallOption) (*fudev1.GetDraftResponse, error) {
	f.record(ctx, in)
	return f.get(in)
}

func (f *fakeFude) GenerateDraft(ctx context.Context, in *fudev1.GenerateDraftRequest, _ ...grpc.CallOption) (*fudev1.GenerateDraftResponse, error) {
	f.record(ctx, in)
	return f.generate(in)
}

func (f *fakeFude) Regenerate(ctx context.Context, in *fudev1.RegenerateRequest, _ ...grpc.CallOption) (*fudev1.RegenerateResponse, error) {
	f.record(ctx, in)
	return &fudev1.RegenerateResponse{Draft: &fudev1.Draft{Id: in.GetDraftId(), State: fudev1.DraftState_DRAFT_STATE_PENDING}}, nil
}

func (f *fakeFude) EditDraft(ctx context.Context, in *fudev1.EditDraftRequest, _ ...grpc.CallOption) (*fudev1.EditDraftResponse, error) {
	f.record(ctx, in)
	return &fudev1.EditDraftResponse{
		Draft:        &fudev1.Draft{Id: in.GetDraftId(), State: fudev1.DraftState_DRAFT_STATE_PENDING, CurrentVersion: 2},
		DraftVersion: &fudev1.DraftVersion{Version: 2, Subject: in.GetSubject(), Body: in.GetBody(), CreatedBy: fudev1.VersionAuthor_VERSION_AUTHOR_USER},
	}, nil
}

func (f *fakeFude) Approve(ctx context.Context, in *fudev1.ApproveRequest, _ ...grpc.CallOption) (*fudev1.ApproveResponse, error) {
	f.record(ctx, in)
	return f.approve(in)
}

func (f *fakeFude) Discard(ctx context.Context, in *fudev1.DiscardRequest, _ ...grpc.CallOption) (*fudev1.DiscardResponse, error) {
	f.record(ctx, in)
	return &fudev1.DiscardResponse{Draft: &fudev1.Draft{Id: in.GetDraftId(), State: fudev1.DraftState_DRAFT_STATE_DISCARDED}}, nil
}

func (f *fakeFude) MarkPosted(ctx context.Context, in *fudev1.MarkPostedRequest, _ ...grpc.CallOption) (*fudev1.MarkPostedResponse, error) {
	f.record(ctx, in)
	if f.markPostedErr != nil {
		return nil, f.markPostedErr
	}
	return &fudev1.MarkPostedResponse{Draft: &fudev1.Draft{Id: in.GetDraftId(), State: fudev1.DraftState_DRAFT_STATE_SENT}}, nil
}

type draftsHarness struct {
	fude   *fakeFude
	client apiv1connect.DraftsServiceClient
	token  string
	owner  string
}

func newDraftsHarness(t *testing.T) *draftsHarness {
	t.Helper()
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
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
	fude := &fakeFude{}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewDraftsServiceHandler(connectapi.NewDraftsServer(fude, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &draftsHarness{fude: fude, token: token, owner: session.OwnerID.String(), client: apiv1connect.NewDraftsServiceClient(srv.Client(), srv.URL)}
}

func codeAndReason(t *testing.T, err error) (connect.Code, string) {
	t.Helper()
	var ce *connect.Error
	if !asConnectError(err, &ce) {
		t.Fatalf("got %v, want a connect error", err)
	}
	for _, d := range ce.Details() {
		if v, derr := d.Value(); derr == nil {
			if info, ok := v.(*errdetails.ErrorInfo); ok {
				return ce.Code(), info.GetReason()
			}
		}
	}
	return ce.Code(), ""
}

func withInfo(code codes.Code, reason string) error {
	st, _ := status.New(code, "msg").WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: "fude.shogun"})
	return st.Err()
}

func TestListQueueMapsStateAndShowsSubjectAndPreview(t *testing.T) {
	h := newDraftsHarness(t)
	h.fude.list = func(*fudev1.ListQueueRequest) (*fudev1.ListQueueResponse, error) {
		return &fudev1.ListQueueResponse{
			Drafts: []*fudev1.Draft{{
				Id: "d1", Kind: fudev1.DraftKind_DRAFT_KIND_COVER_LETTER, TargetType: fudev1.TargetType_TARGET_TYPE_JOB, TargetId: "j1",
				Channel: fudev1.Channel_CHANNEL_EMAIL, State: fudev1.DraftState_DRAFT_STATE_PENDING, CurrentVersion: 2,
				Recipient: "jobs@lumen.example", Version: 5, Subject: "Hello", Preview: "Dear team",
			}},
			NextPageToken: "next",
		}, nil
	}

	resp, err := h.client.ListQueue(context.Background(), withCookie(connect.NewRequest(&apiv1.ListQueueRequest{
		State: apiv1.DraftState_DRAFT_STATE_PENDING, PageSize: 20, PageToken: "tok",
	}), h.token))
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	d := resp.Msg.GetDrafts()[0]
	if d.GetKind() != apiv1.DraftKind_DRAFT_KIND_COVER_LETTER || d.GetTarget() != apiv1.DraftTarget_DRAFT_TARGET_JOB ||
		d.GetChannel() != apiv1.DraftChannel_DRAFT_CHANNEL_EMAIL || d.GetState() != apiv1.DraftState_DRAFT_STATE_PENDING ||
		d.GetSubject() != "Hello" || d.GetPreview() != "Dear team" || d.GetVersion() != 5 || d.GetRecipient() != "jobs@lumen.example" ||
		resp.Msg.GetNextPageToken() != "next" {
		t.Fatalf("got %v", resp.Msg)
	}
	sent := h.fude.last().(*fudev1.ListQueueRequest)
	if sent.GetState() != fudev1.DraftState_DRAFT_STATE_PENDING || sent.GetPageSize() != 20 || sent.GetPageToken() != "tok" {
		t.Fatalf("fude was asked %v", sent)
	}
	if h.fude.owners[0] != h.owner {
		t.Fatalf("fude call acted for %q, want the signed-in owner", h.fude.owners[0])
	}
}

func TestGetDraftReturnsVersionsWithTheirAuthor(t *testing.T) {
	h := newDraftsHarness(t)
	h.fude.get = func(in *fudev1.GetDraftRequest) (*fudev1.GetDraftResponse, error) {
		return &fudev1.GetDraftResponse{
			Draft: &fudev1.Draft{Id: in.GetDraftId(), State: fudev1.DraftState_DRAFT_STATE_PENDING, CurrentVersion: 2},
			Versions: []*fudev1.DraftVersion{
				{Version: 2, Subject: "S2", Body: "edited", CreatedBy: fudev1.VersionAuthor_VERSION_AUTHOR_USER},
				{Version: 1, Subject: "S1", Body: "first", CreatedBy: fudev1.VersionAuthor_VERSION_AUTHOR_AI, ExtraContext: "mention Go"},
			},
		}, nil
	}

	resp, err := h.client.GetDraft(context.Background(), withCookie(connect.NewRequest(&apiv1.GetDraftRequest{Id: "d1"}), h.token))

	vs := resp.Msg.GetVersions()
	if err != nil || len(vs) != 2 || vs[0].GetAuthor() != apiv1.VersionAuthor_VERSION_AUTHOR_USER ||
		vs[1].GetAuthor() != apiv1.VersionAuthor_VERSION_AUTHOR_AI || vs[1].GetExtraContext() != "mention Go" || vs[0].GetBody() != "edited" {
		t.Fatalf("got %v, %v", resp.Msg, err)
	}
}

func TestGenerateDraftPassesTheIdempotencyKeyAndMapsTheTarget(t *testing.T) {
	h := newDraftsHarness(t)
	h.fude.generate = func(*fudev1.GenerateDraftRequest) (*fudev1.GenerateDraftResponse, error) {
		return &fudev1.GenerateDraftResponse{Draft: &fudev1.Draft{Id: "d9", State: fudev1.DraftState_DRAFT_STATE_GENERATING}}, nil
	}
	req := connect.NewRequest(&apiv1.GenerateDraftRequest{
		Kind: apiv1.DraftKind_DRAFT_KIND_OUTREACH, Target: apiv1.DraftTarget_DRAFT_TARGET_CONTACT, TargetId: "c1",
		Channel: apiv1.DraftChannel_DRAFT_CHANNEL_LINKEDIN, ExtraContext: "keep it short",
	})
	req.Header().Set("Idempotency-Key", "key-1")

	resp, err := h.client.GenerateDraft(context.Background(), withCookie(req, h.token))

	if err != nil || resp.Msg.GetDraft().GetState() != apiv1.DraftState_DRAFT_STATE_GENERATING {
		t.Fatalf("got %v, %v", resp.Msg, err)
	}
	sent := h.fude.last().(*fudev1.GenerateDraftRequest)
	if sent.GetIdempotencyKey() != "key-1" || sent.GetKind() != fudev1.DraftKind_DRAFT_KIND_OUTREACH ||
		sent.GetTargetType() != fudev1.TargetType_TARGET_TYPE_CONTACT || sent.GetChannel() != fudev1.Channel_CHANNEL_LINKEDIN ||
		sent.GetTargetId() != "c1" || sent.GetExtraContext() != "keep it short" {
		t.Fatalf("fude was asked %v", sent)
	}
}

func TestEditRegenerateAndDiscardPassTheVersionAndIDThrough(t *testing.T) {
	h := newDraftsHarness(t)

	edit, editErr := h.client.EditDraft(context.Background(), withCookie(connect.NewRequest(&apiv1.EditDraftRequest{Id: "d1", Subject: "S", Body: "B", Version: 4}), h.token))
	editSent := h.fude.last().(*fudev1.EditDraftRequest)
	regen, regenErr := h.client.Regenerate(context.Background(), withCookie(connect.NewRequest(&apiv1.RegenerateRequest{Id: "d1", ExtraContext: "shorter", Version: 4}), h.token))
	regenSent := h.fude.last().(*fudev1.RegenerateRequest)
	discard, discardErr := h.client.Discard(context.Background(), withCookie(connect.NewRequest(&apiv1.DiscardRequest{Id: "d1", Version: 4}), h.token))
	discardSent := h.fude.last().(*fudev1.DiscardRequest)

	if editErr != nil || regenErr != nil || discardErr != nil {
		t.Fatalf("errors %v %v %v", editErr, regenErr, discardErr)
	}
	if editSent.GetDraftId() != "d1" || editSent.GetVersion() != 4 || editSent.GetBody() != "B" || edit.Msg.GetDraftVersion().GetBody() != "B" ||
		edit.Msg.GetDraftVersion().GetAuthor() != apiv1.VersionAuthor_VERSION_AUTHOR_USER {
		t.Fatalf("edit: sent %v, got %v", editSent, edit.Msg)
	}
	if regenSent.GetVersion() != 4 || regenSent.GetExtraContext() != "shorter" || regen.Msg.GetDraft().GetId() != "d1" {
		t.Fatalf("regenerate: sent %v", regenSent)
	}
	if discardSent.GetVersion() != 4 || discard.Msg.GetDraft().GetState() != apiv1.DraftState_DRAFT_STATE_DISCARDED {
		t.Fatalf("discard: sent %v, got %v", discardSent, discard.Msg)
	}
}

func TestApprovePassesTheExactDigestAndSaysWhetherToCopy(t *testing.T) {
	h := newDraftsHarness(t)
	h.fude.approve = func(in *fudev1.ApproveRequest) (*fudev1.ApproveResponse, error) {
		return &fudev1.ApproveResponse{Draft: &fudev1.Draft{Id: in.GetDraftId(), State: fudev1.DraftState_DRAFT_STATE_APPROVED}, CopyReady: true}, nil
	}
	digest := make([]byte, 32)
	digest[0] = 7

	resp, err := h.client.Approve(context.Background(), withCookie(connect.NewRequest(&apiv1.ApproveRequest{Id: "d1", Version: 3, BodySha256: digest}), h.token))

	sent := h.fude.last().(*fudev1.ApproveRequest)
	if err != nil || !resp.Msg.GetCopyReady() || resp.Msg.GetDraft().GetState() != apiv1.DraftState_DRAFT_STATE_APPROVED ||
		sent.GetVersion() != 3 || len(sent.GetBodySha256()) != 32 || sent.GetBodySha256()[0] != 7 {
		t.Fatalf("got %v, %v, sent %v", resp.Msg, err, sent)
	}
}

func TestApproveKeepsTheReasonsThatTellTheOwnerWhatHappenedToTheMail(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		code   connect.Code
		reason string
	}{
		{"stale version", withInfo(codes.Aborted, "VERSION_CONFLICT"), connect.CodeAborted, "VERSION_CONFLICT"},
		{"text changed", withInfo(codes.InvalidArgument, "BODY_HASH_MISMATCH"), connect.CodeInvalidArgument, "BODY_HASH_MISMATCH"},
		{"no recipient", withInfo(codes.InvalidArgument, "RECIPIENT_REQUIRED"), connect.CodeInvalidArgument, "RECIPIENT_REQUIRED"},
		{"send refused, draft back in the queue", withInfo(codes.FailedPrecondition, "SEND_REFUSED"), connect.CodeFailedPrecondition, "SEND_REFUSED"},
		{"provider did not send", withInfo(codes.FailedPrecondition, "SEND_FAILED"), connect.CodeFailedPrecondition, "SEND_FAILED"},
		{"mail service down, draft back in the queue", withInfo(codes.Unavailable, "SEND_UNAVAILABLE"), connect.CodeUnavailable, "SEND_UNAVAILABLE"},
		{"not known whether the mail went out", withInfo(codes.Unavailable, "SEND_STATUS_UNKNOWN"), connect.CodeUnavailable, "SEND_STATUS_UNKNOWN"},
		{"fude itself unreachable", status.Error(codes.Unavailable, "down"), connect.CodeUnavailable, "UNAVAILABLE"},
		{"a bug in fude", status.Error(codes.Internal, "secret internals"), connect.CodeInternal, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newDraftsHarness(t)
			h.fude.approve = func(*fudev1.ApproveRequest) (*fudev1.ApproveResponse, error) { return nil, tt.err }

			_, err := h.client.Approve(context.Background(), withCookie(connect.NewRequest(&apiv1.ApproveRequest{Id: "d1", Version: 1, BodySha256: make([]byte, 32)}), h.token))

			code, reason := codeAndReason(t, err)
			if code != tt.code || reason != tt.reason {
				t.Fatalf("got %s %q, want %s %q", code, reason, tt.code, tt.reason)
			}
			if tt.code == connect.CodeInternal && err != nil && contains(err.Error(), "secret internals") {
				t.Fatalf("internals leaked: %v", err)
			}
		})
	}
}

func TestDraftsRequireASession(t *testing.T) {
	h := newDraftsHarness(t)

	_, err := h.client.ListQueue(context.Background(), connect.NewRequest(&apiv1.ListQueueRequest{}))

	if code, _ := codeAndReason(t, err); code != connect.CodeUnauthenticated {
		t.Fatalf("got %v, want Unauthenticated", err)
	}
	if len(h.fude.requests) != 0 {
		t.Fatal("fude was called without a session")
	}
}

func asConnectError(err error, target **connect.Error) bool { return errors.As(err, target) }

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestMarkPostedPassesTheVersionThroughAndKeepsTheRefusalReason(t *testing.T) {
	h := newDraftsHarness(t)

	ok, err := h.client.MarkPosted(context.Background(), withCookie(connect.NewRequest(&apiv1.MarkPostedRequest{Id: "d1", Version: 4}), h.token))
	sent := h.fude.last().(*fudev1.MarkPostedRequest)
	h.fude.markPostedErr = withInfo(codes.FailedPrecondition, "DRAFT_CHANNEL_NOT_COPY_ONLY")
	_, refused := h.client.MarkPosted(context.Background(), withCookie(connect.NewRequest(&apiv1.MarkPostedRequest{Id: "d2", Version: 1}), h.token))

	if err != nil || sent.GetDraftId() != "d1" || sent.GetVersion() != 4 || ok.Msg.GetDraft().GetState() != apiv1.DraftState_DRAFT_STATE_SENT {
		t.Fatalf("sent %v, got %v, %v", sent, ok, err)
	}
	if code, reason := codeAndReason(t, refused); code != connect.CodeFailedPrecondition || reason != "DRAFT_CHANNEL_NOT_COPY_ONLY" {
		t.Fatalf("got %v %q", code, reason)
	}
}
