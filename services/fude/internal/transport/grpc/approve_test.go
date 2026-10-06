package grpc_test

import (
	"testing"

	"google.golang.org/grpc/codes"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/pkg/hanko"
)

// approvable returns a pending draft whose newest version has real text, and
// the digest of that text.
func (h *harness) approvable(t *testing.T, channel fudev1.Channel) (*fudev1.Draft, []byte) {
	t.Helper()
	d := h.pending(t, h.generate(t, ""))
	if _, err := h.pool.Exec(t.Context(), `UPDATE drafts SET channel = $2 WHERE id = $1`, d.GetId(), map[fudev1.Channel]string{
		fudev1.Channel_CHANNEL_EMAIL: "email", fudev1.Channel_CHANNEL_LINKEDIN: "linkedin",
	}[channel]); err != nil {
		t.Fatal(err)
	}
	edited, err := h.client.EditDraft(h.ctx(t), &fudev1.EditDraftRequest{
		DraftId: d.GetId(), Subject: "Hello", Body: "Dear Lumen team", Version: d.GetVersion(),
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	return edited.GetDraft(), hanko.BodyDigest("Hello", "Dear Lumen team")
}

func TestApproveMovesTheDraftToApprovedAndTellsEmailFromCopy(t *testing.T) {
	tests := []struct {
		name     string
		channel  fudev1.Channel
		wantCopy bool
	}{
		{"email is sent", fudev1.Channel_CHANNEL_EMAIL, false},
		{"linkedin is copied", fudev1.Channel_CHANNEL_LINKEDIN, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			d, digest := h.approvable(t, tt.channel)

			res, err := h.client.Approve(h.ctx(t), &fudev1.ApproveRequest{DraftId: d.GetId(), Version: d.GetCurrentVersion(), BodySha256: digest})

			if err != nil || res.GetDraft().GetState() != fudev1.DraftState_DRAFT_STATE_APPROVED || res.GetCopyReady() != tt.wantCopy {
				t.Fatalf("got %+v, %v", res, err)
			}
		})
	}
}

func TestApproveRefusalsCarryStableReasons(t *testing.T) {
	h := newHarness(t)
	d, digest := h.approvable(t, fudev1.Channel_CHANNEL_LINKEDIN)
	version := d.GetCurrentVersion()
	tests := []struct {
		name string
		req  *fudev1.ApproveRequest
		code codes.Code
		why  string
	}{
		{"stale version", &fudev1.ApproveRequest{DraftId: d.GetId(), Version: version - 1, BodySha256: digest}, codes.Aborted, "VERSION_CONFLICT"},
		{"other text", &fudev1.ApproveRequest{DraftId: d.GetId(), Version: version, BodySha256: hanko.BodyDigest("x", "y")}, codes.InvalidArgument, "BODY_HASH_MISMATCH"},
		{"short digest", &fudev1.ApproveRequest{DraftId: d.GetId(), Version: version, BodySha256: []byte{1}}, codes.InvalidArgument, "BODY_HASH_INVALID"},
		{"no version", &fudev1.ApproveRequest{DraftId: d.GetId(), BodySha256: digest}, codes.InvalidArgument, "VERSION_REQUIRED"},
		{"bad id", &fudev1.ApproveRequest{DraftId: "nope", Version: 1, BodySha256: digest}, codes.InvalidArgument, "INVALID_ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.Approve(h.ctx(t), tt.req)

			requireStatus(t, err, tt.code, tt.why)
		})
	}
}

func TestApproveTwiceIsFailedPrecondition(t *testing.T) {
	h := newHarness(t)
	d, digest := h.approvable(t, fudev1.Channel_CHANNEL_LINKEDIN)
	req := &fudev1.ApproveRequest{DraftId: d.GetId(), Version: d.GetCurrentVersion(), BodySha256: digest}
	if _, err := h.client.Approve(h.ctx(t), req); err != nil {
		t.Fatal(err)
	}

	_, err := h.client.Approve(h.ctx(t), req)

	requireStatus(t, err, codes.FailedPrecondition, "DRAFT_STATE_INVALID_TRANSITION")
}

func TestEditAfterApprovalNeedsAFreshApproval(t *testing.T) {
	h := newHarness(t)
	d, digest := h.approvable(t, fudev1.Channel_CHANNEL_LINKEDIN)
	approved, err := h.client.Approve(h.ctx(t), &fudev1.ApproveRequest{DraftId: d.GetId(), Version: d.GetCurrentVersion(), BodySha256: digest})
	if err != nil {
		t.Fatal(err)
	}

	edited, err := h.client.EditDraft(h.ctx(t), &fudev1.EditDraftRequest{
		DraftId: d.GetId(), Subject: "Hello", Body: "A changed letter", Version: approved.GetDraft().GetVersion(),
	})
	if err != nil || edited.GetDraft().GetState() != fudev1.DraftState_DRAFT_STATE_PENDING {
		t.Fatalf("got %+v, %v", edited.GetDraft(), err)
	}
	// The old approval does not cover the new text.
	_, staleErr := h.client.Approve(h.ctx(t), &fudev1.ApproveRequest{DraftId: d.GetId(), Version: edited.GetDraft().GetCurrentVersion(), BodySha256: digest})
	fresh, freshErr := h.client.Approve(h.ctx(t), &fudev1.ApproveRequest{
		DraftId: d.GetId(), Version: edited.GetDraft().GetCurrentVersion(), BodySha256: hanko.BodyDigest("Hello", "A changed letter"),
	})

	requireStatus(t, staleErr, codes.InvalidArgument, "BODY_HASH_MISMATCH")
	if freshErr != nil || fresh.GetDraft().GetState() != fudev1.DraftState_DRAFT_STATE_APPROVED {
		t.Fatalf("fresh approval: %+v, %v", fresh, freshErr)
	}
	var approvals int
	if err := h.pool.QueryRow(t.Context(), `SELECT count(*) FROM approvals WHERE draft_id = $1`, d.GetId()).Scan(&approvals); err != nil || approvals != 2 {
		t.Fatalf("got %d approvals, err %v; want one per approval", approvals, err)
	}
}
