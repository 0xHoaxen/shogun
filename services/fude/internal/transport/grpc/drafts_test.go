package grpc_test

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/pkg/hanko"
)

func (h *harness) generate(t *testing.T, key string) *fudev1.Draft {
	t.Helper()
	res, err := h.client.GenerateDraft(h.ctx(t), &fudev1.GenerateDraftRequest{
		Kind: fudev1.DraftKind_DRAFT_KIND_COVER_LETTER, TargetType: fudev1.TargetType_TARGET_TYPE_JOB,
		TargetId: "0194f0a0-0000-7000-8000-000000000001", Channel: fudev1.Channel_CHANNEL_EMAIL,
		Recipient: "jobs@lumen.example", ExtraContext: "mention Go", IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return res.GetDraft()
}

// pending moves a draft to pending with a first AI version, as the generation
// job will, so edit, approve and discard have something to act on.
func (h *harness) pending(t *testing.T, d *fudev1.Draft) *fudev1.Draft {
	t.Helper()
	ctx := t.Context()
	if _, err := h.pool.Exec(ctx, `INSERT INTO draft_versions (draft_id, version, body, body_sha256, created_by)
		VALUES ($1, 1, 'Dear team', '\x01', 'ai')`, d.GetId()); err != nil {
		t.Fatalf("seed version: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE drafts SET state = 'pending', current_version = 1, version = version + 1 WHERE id = $1`, d.GetId()); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	got, err := h.client.GetDraft(h.ctx(t), &fudev1.GetDraftRequest{DraftId: d.GetId()})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return got.GetDraft()
}

func TestGenerateDraftCreatesGeneratingDraftAndQueuesVersionOne(t *testing.T) {
	h := newHarness(t)

	d := h.generate(t, "")

	if d.GetState() != fudev1.DraftState_DRAFT_STATE_GENERATING || d.GetCurrentVersion() != 0 || d.GetVersion() != 1 ||
		d.GetRecipient() != "jobs@lumen.example" {
		t.Fatalf("got %+v", d)
	}
	jobs := h.queue.enqueued()
	if len(jobs) != 1 || jobs[0].Version != 1 || jobs[0].ExtraContext != "mention Go" || jobs[0].DraftID.String() != d.GetId() {
		t.Fatalf("got jobs %+v", jobs)
	}
}

func TestGenerateDraftReplaysIdempotencyKey(t *testing.T) {
	h := newHarness(t)

	first := h.generate(t, "gen-1")
	again := h.generate(t, "gen-1")

	if first.GetId() != again.GetId() {
		t.Fatalf("replay returned %s, want %s", again.GetId(), first.GetId())
	}
	if n := len(h.queue.enqueued()); n != 1 {
		t.Fatalf("got %d jobs after a replay, want 1", n)
	}
}

func TestGenerateDraftRollsBackWhenQueueingFails(t *testing.T) {
	h := newHarness(t)
	h.queue.err = errors.New("river down")

	_, err := h.client.GenerateDraft(h.ctx(t), &fudev1.GenerateDraftRequest{
		Kind: fudev1.DraftKind_DRAFT_KIND_POST, TargetType: fudev1.TargetType_TARGET_TYPE_NONE, Channel: fudev1.Channel_CHANNEL_LINKEDIN,
	})

	requireStatus(t, err, codes.Internal, "INTERNAL")
	var n int
	if err := h.pool.QueryRow(t.Context(), `SELECT count(*) FROM drafts`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("got %d drafts, err %v; want the insert rolled back", n, err)
	}
}

func TestGenerateDraftRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	ok := &fudev1.GenerateDraftRequest{
		Kind: fudev1.DraftKind_DRAFT_KIND_OUTREACH, TargetType: fudev1.TargetType_TARGET_TYPE_CONTACT,
		TargetId: "0194f0a0-0000-7000-8000-000000000002", Channel: fudev1.Channel_CHANNEL_EMAIL, Recipient: "a@b.example",
	}
	tests := []struct {
		name   string
		mutate func(r *fudev1.GenerateDraftRequest)
		reason string
	}{
		{"missing kind", func(r *fudev1.GenerateDraftRequest) { r.Kind = 0 }, "INVALID_KIND"},
		{"missing target type", func(r *fudev1.GenerateDraftRequest) { r.TargetType = 0 }, "INVALID_TARGET_TYPE"},
		{"missing channel", func(r *fudev1.GenerateDraftRequest) { r.Channel = 0 }, "INVALID_CHANNEL"},
		{"target without id", func(r *fudev1.GenerateDraftRequest) { r.TargetId = "" }, "INVALID_ID"},
		{"none target with id", func(r *fudev1.GenerateDraftRequest) { r.TargetType = fudev1.TargetType_TARGET_TYPE_NONE }, "TARGET_ID_UNEXPECTED"},
		{"email without recipient", func(r *fudev1.GenerateDraftRequest) { r.Recipient = "" }, "INVALID_RECIPIENT"},
		{"recipient with display name", func(r *fudev1.GenerateDraftRequest) { r.Recipient = "Bob <b@c.example>" }, "INVALID_RECIPIENT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := cloneRequest(ok)
			tt.mutate(req)

			_, err := h.client.GenerateDraft(h.ctx(t), req)

			requireStatus(t, err, codes.InvalidArgument, tt.reason)
		})
	}
	if n := len(h.queue.enqueued()); n != 0 {
		t.Fatalf("got %d jobs for rejected input", n)
	}
}

func cloneRequest(r *fudev1.GenerateDraftRequest) *fudev1.GenerateDraftRequest {
	return proto.Clone(r).(*fudev1.GenerateDraftRequest)
}

func TestCallsWithoutIdentityAreRejected(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.ListQueue(t.Context(), &fudev1.ListQueueRequest{})

	requireStatus(t, err, codes.Unauthenticated, "")
}

func TestAnotherOwnerCannotSeeADraft(t *testing.T) {
	h := newHarness(t)
	d := h.generate(t, "")

	_, err := h.client.GetDraft(h.ctxFor(t, "0194f0a0-0000-7000-8000-0000000000aa"), &fudev1.GetDraftRequest{DraftId: d.GetId()})

	requireStatus(t, err, codes.NotFound, "RESOURCE_NOT_FOUND")
}

func TestRegenerateQueuesNextVersionAndKeepsDraftPending(t *testing.T) {
	h := newHarness(t)
	d := h.pending(t, h.generate(t, ""))

	res, err := h.client.Regenerate(h.ctx(t), &fudev1.RegenerateRequest{DraftId: d.GetId(), ExtraContext: "shorter", Version: d.GetVersion()})

	if err != nil || res.GetDraft().GetState() != fudev1.DraftState_DRAFT_STATE_PENDING || res.GetDraft().GetCurrentVersion() != 1 {
		t.Fatalf("got %+v, %v", res.GetDraft(), err)
	}
	jobs := h.queue.enqueued()
	if last := jobs[len(jobs)-1]; last.Version != 2 || last.ExtraContext != "shorter" {
		t.Fatalf("got job %+v, want version 2 with the extra context", last)
	}
	_, err = h.client.Regenerate(h.ctx(t), &fudev1.RegenerateRequest{DraftId: d.GetId(), Version: d.GetVersion()})
	requireStatus(t, err, codes.Aborted, "VERSION_CONFLICT")
}

func TestRegenerateRefusesADraftThatIsStillGenerating(t *testing.T) {
	h := newHarness(t)
	d := h.generate(t, "")

	_, err := h.client.Regenerate(h.ctx(t), &fudev1.RegenerateRequest{DraftId: d.GetId(), Version: d.GetVersion()})

	requireStatus(t, err, codes.FailedPrecondition, "DRAFT_STATE_INVALID_TRANSITION")
}

func TestEditDraftAddsAUserVersionWithItsDigest(t *testing.T) {
	h := newHarness(t)
	d := h.pending(t, h.generate(t, ""))

	res, err := h.client.EditDraft(h.ctx(t), &fudev1.EditDraftRequest{
		DraftId: d.GetId(), Subject: "Hello", Body: "  Dear Lumen team  ", Version: d.GetVersion(),
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	v := res.GetDraftVersion()
	if res.GetDraft().GetCurrentVersion() != 2 || v.GetVersion() != 2 || v.GetBody() != "Dear Lumen team" ||
		v.GetCreatedBy() != fudev1.VersionAuthor_VERSION_AUTHOR_USER ||
		string(v.GetBodySha256()) != string(hanko.BodyDigest("Hello", "Dear Lumen team")) {
		t.Fatalf("got draft %+v version %+v", res.GetDraft(), v)
	}
	got, _ := h.client.GetDraft(h.ctx(t), &fudev1.GetDraftRequest{DraftId: d.GetId()})
	if len(got.GetVersions()) != 2 || got.GetVersions()[0].GetVersion() != 2 {
		t.Fatalf("want two versions, newest first, got %+v", got.GetVersions())
	}
}

func TestEditDraftAfterApprovalReturnsToPending(t *testing.T) {
	h := newHarness(t)
	d := h.pending(t, h.generate(t, ""))
	if _, err := h.pool.Exec(t.Context(), `UPDATE drafts SET state = 'approved', version = version + 1 WHERE id = $1`, d.GetId()); err != nil {
		t.Fatal(err)
	}

	res, err := h.client.EditDraft(h.ctx(t), &fudev1.EditDraftRequest{DraftId: d.GetId(), Body: "Changed", Version: d.GetVersion() + 1})

	if err != nil || res.GetDraft().GetState() != fudev1.DraftState_DRAFT_STATE_PENDING {
		t.Fatalf("got %+v, %v", res.GetDraft(), err)
	}
}

func TestEditDraftRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	d := h.pending(t, h.generate(t, ""))
	tests := []struct {
		name string
		req  *fudev1.EditDraftRequest
		code codes.Code
		why  string
	}{
		{"empty body", &fudev1.EditDraftRequest{DraftId: d.GetId(), Body: " ", Version: d.GetVersion()}, codes.InvalidArgument, "BODY_REQUIRED"},
		{"no version", &fudev1.EditDraftRequest{DraftId: d.GetId(), Body: "x"}, codes.InvalidArgument, "VERSION_REQUIRED"},
		{"bad id", &fudev1.EditDraftRequest{DraftId: "nope", Body: "x", Version: 1}, codes.InvalidArgument, "INVALID_ID"},
		{"stale version", &fudev1.EditDraftRequest{DraftId: d.GetId(), Body: "x", Version: d.GetVersion() + 5}, codes.Aborted, "VERSION_CONFLICT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.EditDraft(h.ctx(t), tt.req)

			requireStatus(t, err, tt.code, tt.why)
		})
	}
}

func TestDiscardMovesPendingToDiscardedOnly(t *testing.T) {
	h := newHarness(t)
	d := h.pending(t, h.generate(t, ""))

	res, err := h.client.Discard(h.ctx(t), &fudev1.DiscardRequest{DraftId: d.GetId(), Version: d.GetVersion()})

	if err != nil || res.GetDraft().GetState() != fudev1.DraftState_DRAFT_STATE_DISCARDED {
		t.Fatalf("got %+v, %v", res.GetDraft(), err)
	}
	_, err = h.client.Discard(h.ctx(t), &fudev1.DiscardRequest{DraftId: d.GetId(), Version: res.GetDraft().GetVersion()})
	requireStatus(t, err, codes.FailedPrecondition, "DRAFT_STATE_INVALID_TRANSITION")
}

func TestListQueueDefaultsToPendingAndPages(t *testing.T) {
	h := newHarness(t)
	a := h.pending(t, h.generate(t, ""))
	b := h.pending(t, h.generate(t, ""))
	h.generate(t, "") // generating, not in the default queue

	page1, err := h.client.ListQueue(h.ctx(t), &fudev1.ListQueueRequest{PageSize: 1})
	if err != nil || len(page1.GetDrafts()) != 1 || page1.GetNextPageToken() == "" {
		t.Fatalf("page1 %+v, %v", page1, err)
	}
	page2, err := h.client.ListQueue(h.ctx(t), &fudev1.ListQueueRequest{PageSize: 1, PageToken: page1.GetNextPageToken()})

	if err != nil || len(page2.GetDrafts()) != 1 || page2.GetNextPageToken() != "" {
		t.Fatalf("page2 %+v, %v", page2, err)
	}
	got := []string{page1.GetDrafts()[0].GetId(), page2.GetDrafts()[0].GetId()}
	if got[0] != b.GetId() || got[1] != a.GetId() {
		t.Fatalf("got order %v, want newest first (%s, %s)", got, b.GetId(), a.GetId())
	}
	gen, _ := h.client.ListQueue(h.ctx(t), &fudev1.ListQueueRequest{State: fudev1.DraftState_DRAFT_STATE_GENERATING})
	if len(gen.GetDrafts()) != 1 {
		t.Fatalf("want one generating draft, got %d", len(gen.GetDrafts()))
	}
	_, err = h.client.ListQueue(h.ctx(t), &fudev1.ListQueueRequest{PageToken: "!!"})
	requireStatus(t, err, codes.InvalidArgument, "INVALID_PAGE_TOKEN")
}

func TestAddVoiceSample(t *testing.T) {
	h := newHarness(t)

	res, err := h.client.AddVoiceSample(h.ctx(t), &fudev1.AddVoiceSampleRequest{Channel: fudev1.Channel_CHANNEL_EMAIL, Text: "  Hi there  "})

	if err != nil || res.GetSample().GetText() != "Hi there" || res.GetSample().GetChannel() != fudev1.Channel_CHANNEL_EMAIL {
		t.Fatalf("got %+v, %v", res.GetSample(), err)
	}
	if len(h.queue.embeds) != 1 || h.queue.embeds[0].String() != res.GetSample().GetId() {
		t.Fatalf("got embed jobs %v, want one for %s", h.queue.embeds, res.GetSample().GetId())
	}
	_, err = h.client.AddVoiceSample(h.ctx(t), &fudev1.AddVoiceSampleRequest{Channel: fudev1.Channel_CHANNEL_EMAIL, Text: " "})
	requireStatus(t, err, codes.InvalidArgument, "TEXT_REQUIRED")
	_, err = h.client.AddVoiceSample(h.ctx(t), &fudev1.AddVoiceSampleRequest{Text: "x"})
	requireStatus(t, err, codes.InvalidArgument, "INVALID_CHANNEL")
}
