package app_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/hanko"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
)

const keyID = "test-1"

type approval struct {
	env      *env
	approver *app.Approver
	pub      ed25519.PublicKey
	priv     ed25519.PrivateKey
}

func newApproval(t *testing.T) *approval {
	t.Helper()
	e := newEnv(t, fakeSource{})
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.NewApprover(e.pool, priv, keyID, func() time.Time { return e.now })
	if err != nil {
		t.Fatal(err)
	}
	return &approval{env: e, approver: a, pub: pub, priv: priv}
}

// pendingDraft stores a pending draft whose newest version has real text.
func (a *approval) pendingDraft(t *testing.T, channel string, recipient *string) (db.Draft, string, string) {
	t.Helper()
	ctx := context.Background()
	target := store.NewID()
	d, err := store.New(a.env.pool).InsertDraft(ctx, db.InsertDraftParams{
		ID: store.NewID(), OwnerID: a.env.owner, Kind: "cover_letter", TargetType: "job", TargetID: &target,
		Channel: channel, Recipient: recipient,
	})
	if err != nil {
		t.Fatal(err)
	}
	subject, body := "Hello Lumen", "Dear Lumen team,\nI would like to help."
	if _, err := store.New(a.env.pool).InsertDraftVersion(ctx, db.InsertDraftVersionParams{
		DraftID: d.ID, Version: 1, Subject: &subject, Body: body, BodySha256: hanko.BodyDigest(subject, body), CreatedBy: "ai",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.env.pool.Exec(ctx, `UPDATE drafts SET state = 'pending', current_version = 1 WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	d, _ = store.New(a.env.pool).GetDraft(ctx, a.env.owner, d.ID)
	return d, subject, body
}

func (a *approval) pubPriv() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return a.pub, a.priv, nil
}

func (a *approval) ctx() context.Context {
	return authz.WithIdentity(context.Background(), authz.Identity{OwnerID: a.env.owner.String(), RequestID: "t"})
}

func strp(s string) *string { return &s }

func TestApproveStampsATokenForExactlyThatVersionTextAndRecipient(t *testing.T) {
	a := newApproval(t)
	d, subject, body := a.pendingDraft(t, "email", strp("jobs@lumen.example"))
	digest := hanko.BodyDigest(subject, body)

	res, err := a.approver.Approve(a.ctx(), app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: digest})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if res.Draft.State != "approved" || res.CopyReady || res.Token == "" {
		t.Fatalf("got %+v", res)
	}
	claims, err := hanko.Verify(res.Token, map[string]ed25519.PublicKey{keyID: a.pub}, hanko.Expected{
		Audience: "tsubame", Issuer: "fude", DraftID: d.ID.String(), Version: 1,
		BodySHA256: hex.EncodeToString(digest), RcptSHA256: hex.EncodeToString(hanko.RecipientDigest([]string{"jobs@lumen.example"})),
	}, hanko.WithClock(func() time.Time { return a.env.now }))
	if err != nil || claims.Subject != a.env.owner.String() {
		t.Fatalf("token does not verify for this draft: %v (%+v)", err, claims)
	}
	if n := a.env.count(t, `SELECT count(*) FROM approvals WHERE draft_id = $1 AND version = 1 AND approved_by = $2`, d.ID, a.env.owner); n != 1 {
		t.Fatalf("got %d approvals, want 1", n)
	}
	var jti uuid.UUID
	if err := a.env.pool.QueryRow(context.Background(), `SELECT token_jti FROM approvals`).Scan(&jti); err != nil || jti.String() == "" {
		t.Fatalf("jti %v, err %v", jti, err)
	}
	if n := a.env.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.approved'`); n != 1 {
		t.Fatalf("got %d draft.approved rows, want 1", n)
	}
}

func TestApproveOfACopyOnlyChannelIsCopyReadyAndNeedsNoRecipient(t *testing.T) {
	a := newApproval(t)
	d, subject, body := a.pendingDraft(t, "linkedin", nil)

	res, err := a.approver.Approve(a.ctx(), app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: hanko.BodyDigest(subject, body)})

	if err != nil || !res.CopyReady || res.Draft.State != "approved" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestApproveRefusesAndChangesNothing(t *testing.T) {
	a := newApproval(t)
	good := func(d db.Draft, subject, body string) app.ApproveInput {
		return app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: hanko.BodyDigest(subject, body)}
	}
	tests := []struct {
		name    string
		prepare func(t *testing.T) app.ApproveInput
		check   func(error) bool
	}{
		{"digest of other text", func(t *testing.T) app.ApproveInput {
			d, _, _ := a.pendingDraft(t, "email", strp("x@y.example"))
			return app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: hanko.BodyDigest("other", "text")}
		}, isInvalid("BODY_HASH_MISMATCH")},
		{"digest of the wrong length", func(t *testing.T) app.ApproveInput {
			d, _, _ := a.pendingDraft(t, "email", strp("x@y.example"))
			return app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: []byte{1, 2}}
		}, isInvalid("BODY_HASH_INVALID")},
		{"a version that is not the newest", func(t *testing.T) app.ApproveInput {
			d, s, b := a.pendingDraft(t, "email", strp("x@y.example"))
			in := good(d, s, b)
			in.Version = 2
			return in
		}, func(err error) bool { return errors.Is(err, store.ErrVersionConflict) }},
		{"an email draft with no recipient", func(t *testing.T) app.ApproveInput {
			d, s, b := a.pendingDraft(t, "email", nil)
			return good(d, s, b)
		}, isInvalid("RECIPIENT_REQUIRED")},
		{"a draft still generating", func(t *testing.T) app.ApproveInput {
			d := a.env.newDraft(t, "cover_letter", "email")
			return app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: hanko.BodyDigest("", "")}
		}, isTransition},
		{"a draft that is not there", func(*testing.T) app.ApproveInput {
			return app.ApproveInput{DraftID: uuid.NewString(), Version: 1, BodySHA256: hanko.BodyDigest("", "")}
		}, func(err error) bool { return errors.Is(err, store.ErrNotFound) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.prepare(t)

			res, err := a.approver.Approve(a.ctx(), in)

			if err == nil || !tt.check(err) || res.Token != "" {
				t.Fatalf("got %+v, %v", res, err)
			}
			if n := a.env.count(t, `SELECT count(*) FROM approvals`); n != 0 {
				t.Fatalf("got %d approvals after a refusal", n)
			}
			if n := a.env.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.approved'`); n != 0 {
				t.Fatalf("got %d draft.approved rows after a refusal", n)
			}
			if n := a.env.count(t, `SELECT count(*) FROM drafts WHERE state = 'approved'`); n != 0 {
				t.Fatalf("a refused approval left a draft approved")
			}
		})
	}
}

func TestApproveTwiceIsRefusedTheSecondTime(t *testing.T) {
	a := newApproval(t)
	d, subject, body := a.pendingDraft(t, "email", strp("jobs@lumen.example"))
	in := app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: hanko.BodyDigest(subject, body)}
	if _, err := a.approver.Approve(a.ctx(), in); err != nil {
		t.Fatal(err)
	}

	_, err := a.approver.Approve(a.ctx(), in)

	if !isTransition(err) {
		t.Fatalf("second approve: got %v, want an invalid transition", err)
	}
	if n := a.env.count(t, `SELECT count(*) FROM approvals`); n != 1 {
		t.Fatalf("got %d approvals, want 1", n)
	}
}

func TestApproveNeedsAnOwner(t *testing.T) {
	a := newApproval(t)
	d, subject, body := a.pendingDraft(t, "linkedin", nil)

	_, err := a.approver.Approve(context.Background(), app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: hanko.BodyDigest(subject, body)})

	if !errors.Is(err, app.ErrNoOwner) {
		t.Fatalf("got %v, want ErrNoOwner", err)
	}
}

func TestNewApproverRejectsBadKeys(t *testing.T) {
	a := newApproval(t)
	_, priv, _ := ed25519.GenerateKey(nil)

	_, shortErr := app.NewApprover(a.env.pool, priv[:10], keyID, nil)
	_, kidErr := app.NewApprover(a.env.pool, priv, "", nil)

	if shortErr == nil || kidErr == nil {
		t.Fatalf("got %v and %v, want both refused", shortErr, kidErr)
	}
}

func isInvalid(reason string) func(error) bool {
	return func(err error) bool {
		var ia *app.InvalidArgumentError
		return errors.As(err, &ia) && ia.Reason == reason
	}
}

func isTransition(err error) bool {
	var te *domain.TransitionError
	return errors.As(err, &te)
}

func TestConcurrentApprovalsOfOneDraftSucceedOnce(t *testing.T) {
	a := newApproval(t)
	d, subject, body := a.pendingDraft(t, "email", strp("jobs@lumen.example"))
	in := app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: hanko.BodyDigest(subject, body)}
	const callers = 8
	results := make(chan error, callers)

	for range callers {
		go func() {
			_, err := a.approver.Approve(a.ctx(), in)
			results <- err
		}()
	}

	succeeded := 0
	for range callers {
		if err := <-results; err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("got %d successful approvals, want exactly 1", succeeded)
	}
	if n := a.env.count(t, `SELECT count(*) FROM approvals`); n != 1 {
		t.Fatalf("got %d approvals, want 1", n)
	}
	if n := a.env.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.approved'`); n != 1 {
		t.Fatalf("got %d draft.approved rows, want 1", n)
	}
}
