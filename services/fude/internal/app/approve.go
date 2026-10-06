package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/pkg/hanko"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
	"github.com/0xHoaxen/shogun/services/fude/internal/wire"
)

// This file is the only place in the repository that calls hanko.Sign.
// Nothing else, no worker, schedule or AI step, may stamp a Hanko: a token is
// only ever the result of the owner approving one exact version. A test and a
// CI check enforce it.

const (
	eventDraftApproved = "draft.approved"

	hankoAudience = "tsubame"
	hankoIssuer   = "fude"
	digestLen     = 32
)

// Reasons for approvals refused for what is in the request.
const (
	reasonDigestInvalid   = "BODY_HASH_INVALID"
	reasonDigestMismatch  = "BODY_HASH_MISMATCH"
	reasonRecipientNeeded = "RECIPIENT_REQUIRED"
)

// Approver approves drafts: it is the only holder of the signing key.
type Approver struct {
	pool *pgxpool.Pool
	key  ed25519.PrivateKey
	kid  string
	now  func() time.Time
}

// NewApprover returns an Approver that signs with key, labelled kid. A nil now
// means time.Now.
func NewApprover(pool *pgxpool.Pool, key ed25519.PrivateKey, kid string, now func() time.Time) (*Approver, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("approver: bad signing key length")
	}
	if kid == "" {
		return nil, errors.New("approver: key id is required")
	}
	if now == nil {
		now = time.Now
	}
	return &Approver{pool: pool, key: key, kid: kid, now: now}, nil
}

// ApproveInput is what the owner saw and approves.
type ApproveInput struct {
	DraftID string
	// Version is the draft version the owner saw; it must still be the newest.
	Version int32
	// BodySHA256 is hanko.BodyDigest of the subject and body the owner saw.
	BodySHA256 []byte
}

// ApproveResult is an approved draft and what it takes to send it.
type ApproveResult struct {
	Draft   db.Draft
	Version db.DraftVersion
	// Token is the Hanko for tsubame.Send. It goes to the caller only and is
	// never stored or logged.
	Token string
	// CopyReady is true for channels the owner copies by hand.
	CopyReady bool
}

// Approve stamps a Hanko for the newest version of a pending draft, once the
// owner's digest matches what is stored, and moves the draft to approved. The
// approval and draft.approved are written in one transaction; if anything
// fails no token leaves.
func (a *Approver) Approve(ctx context.Context, in ApproveInput) (ApproveResult, error) {
	owner, id, err := ownerAndDraft(ctx, in.DraftID, in.Version)
	if err != nil {
		return ApproveResult{}, err
	}
	if len(in.BodySHA256) != digestLen {
		return ApproveResult{}, invalidField("body_sha256", reasonDigestInvalid, "body_sha256 must be a %d-byte SHA-256", digestLen)
	}

	var res ApproveResult
	err = postgres.InTx(ctx, a.pool, func(tx pgx.Tx) error {
		var inner error
		res, inner = a.approve(ctx, tx, owner, id, in)
		return inner
	})
	if err != nil {
		return ApproveResult{}, err
	}
	return res, nil
}

func (a *Approver) approve(ctx context.Context, tx pgx.Tx, owner, id uuid.UUID, in ApproveInput) (ApproveResult, error) {
	repo := store.New(tx)
	d, err := repo.GetDraft(ctx, owner, id)
	if err != nil {
		return ApproveResult{}, err
	}
	current := toDomain(d)
	if current.State != domain.DraftPending {
		_, err := current.Move(domain.DraftApproved, a.now()) // not pending: report it as an invalid move
		return ApproveResult{}, err
	}
	if !current.CanApprove(in.Version) {
		return ApproveResult{}, fmt.Errorf("draft version %d is not the newest: %w", in.Version, store.ErrVersionConflict)
	}
	v, err := repo.GetDraftVersion(ctx, id, in.Version)
	if err != nil {
		return ApproveResult{}, err
	}
	digest := hanko.BodyDigest(deref(v.Subject), v.Body)
	if !bytes.Equal(digest, in.BodySHA256) || !bytes.Equal(digest, v.BodySha256) {
		return ApproveResult{}, invalidField("body_sha256", reasonDigestMismatch, "the approved text is not the text of version %d", in.Version)
	}
	recipients, err := recipientsOf(d)
	if err != nil {
		return ApproveResult{}, err
	}

	now := a.now()
	token, claims, err := a.stamp(now, owner, id, in.Version, digest, recipients)
	if err != nil {
		return ApproveResult{}, err
	}
	jti, err := uuid.Parse(claims.ID)
	if err != nil {
		return ApproveResult{}, fmt.Errorf("token id: %w", err)
	}
	if _, err := repo.InsertApproval(ctx, db.InsertApprovalParams{
		ID: store.NewID(), DraftID: id, Version: in.Version, TokenJti: jti,
		ApprovedBy: owner, ApprovedAt: now, ExpiresAt: claims.ExpiresAt,
	}); err != nil {
		return ApproveResult{}, err
	}
	next, err := current.Move(domain.DraftApproved, now)
	if err != nil {
		return ApproveResult{}, err
	}
	saved, err := saveState(ctx, repo, owner, d, next, d.Version)
	if err != nil {
		return ApproveResult{}, err
	}
	if _, err := outbox.Write(ctx, tx, eventSource, eventDraftApproved, id.String(), &fudev1.DraftApproved{
		DraftId: id.String(), Version: in.Version, Channel: wire.ChannelToProto(domain.Channel(saved.Channel)),
	}); err != nil {
		return ApproveResult{}, fmt.Errorf("write %s event: %w", eventDraftApproved, err)
	}
	return ApproveResult{
		Draft: saved, Version: v, Token: token, CopyReady: domain.Channel(saved.Channel) != domain.ChannelEmail,
	}, nil
}

// stamp signs the token for one exact version, text and recipient list.
func (a *Approver) stamp(now time.Time, owner, id uuid.UUID, version int32, digest []byte, recipients []string) (string, hanko.Claims, error) {
	claims, err := hanko.NewClaims(now, hanko.Claims{
		Audience: hankoAudience, Issuer: hankoIssuer, Subject: owner.String(), DraftID: id.String(),
		Version: int64(version), BodySHA256: hex.EncodeToString(digest),
		RcptSHA256: hex.EncodeToString(hanko.RecipientDigest(recipients)),
	})
	if err != nil {
		return "", hanko.Claims{}, fmt.Errorf("claims: %w", err)
	}
	token, err := hanko.Sign(claims, a.key, a.kid)
	if err != nil {
		return "", hanko.Claims{}, fmt.Errorf("sign: %w", err)
	}
	return token, claims, nil
}

// recipientsOf returns who a draft goes to: its recipient for email, and no one
// for the copy-only channels.
func recipientsOf(d db.Draft) ([]string, error) {
	if domain.Channel(d.Channel) != domain.ChannelEmail {
		return nil, nil
	}
	if d.Recipient == nil || *d.Recipient == "" {
		return nil, invalidField("recipient", reasonRecipientNeeded, "an email draft needs a recipient before it can be approved")
	}
	return []string{*d.Recipient}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
