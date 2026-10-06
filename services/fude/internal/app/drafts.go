package app

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/pkg/hanko"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
)

// Input limits.
const (
	maxIdempotencyKeyLen = 200
	maxExtraContextLen   = 4000
	maxSubjectLen        = 300
	maxBodyLen           = 20000
	maxVoiceSampleLen    = 10000

	createdByAI   = "ai"
	createdByUser = "user"
)

// GenerateDraftInput is the input of GenerateDraft.
type GenerateDraftInput struct {
	Kind         domain.Kind
	TargetType   domain.TargetType
	TargetID     string
	Channel      domain.Channel
	ExtraContext string
	// Recipient is the email address when Channel is email.
	Recipient      string
	IdempotencyKey string
}

// DraftDetail is a draft with its versions, newest first.
type DraftDetail struct {
	Draft    db.Draft
	Versions []db.DraftVersion
}

// ListQueueInput is the input of ListQueue. A zero State means pending.
type ListQueueInput struct {
	State     domain.DraftState
	PageSize  int32
	PageToken string
}

// ListQueueResult is one page of drafts.
type ListQueueResult struct {
	Drafts        []db.Draft
	NextPageToken string
}

// GenerateDraft creates a draft in state generating and queues its first
// version. A repeated idempotency key returns the original draft.
func (s *Service) GenerateDraft(ctx context.Context, in GenerateDraftInput) (db.Draft, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Draft{}, err
	}
	params, err := newDraftParams(owner, in)
	if err != nil {
		return db.Draft{}, err
	}

	var res db.Draft
	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		if in.IdempotencyKey != "" {
			replay, getErr := repo.GetDraftByIdempotencyKey(ctx, owner, in.IdempotencyKey)
			if getErr == nil {
				res = replay
				return nil
			}
			if !errors.Is(getErr, store.ErrNotFound) {
				return getErr
			}
		}
		d, insErr := repo.InsertDraft(ctx, params)
		if insErr != nil {
			return insErr
		}
		res = d
		return s.enqueue(ctx, tx, GenerateArgs{DraftID: d.ID, Version: 1, ExtraContext: in.ExtraContext})
	})
	return res, err
}

func newDraftParams(owner uuid.UUID, in GenerateDraftInput) (db.InsertDraftParams, error) {
	switch {
	case !in.Kind.Valid():
		return db.InsertDraftParams{}, invalidField("kind", "INVALID_KIND", "kind is missing or unknown")
	case !in.TargetType.Valid():
		return db.InsertDraftParams{}, invalidField("target_type", "INVALID_TARGET_TYPE", "target type is missing or unknown")
	case !in.Channel.Valid():
		return db.InsertDraftParams{}, invalidField("channel", "INVALID_CHANNEL", "channel is missing or unknown")
	case len(in.ExtraContext) > maxExtraContextLen:
		return db.InsertDraftParams{}, invalidField("extra_context", "EXTRA_CONTEXT_TOO_LONG", "extra context is longer than %d bytes", maxExtraContextLen)
	case len(in.IdempotencyKey) > maxIdempotencyKeyLen:
		return db.InsertDraftParams{}, invalidField("idempotency_key", "INVALID_IDEMPOTENCY_KEY", "idempotency key is longer than %d bytes", maxIdempotencyKeyLen)
	}
	target, err := parseTarget(in.TargetType, in.TargetID)
	if err != nil {
		return db.InsertDraftParams{}, err
	}
	recipient, err := parseRecipient(in.Channel, in.Recipient)
	if err != nil {
		return db.InsertDraftParams{}, err
	}
	return db.InsertDraftParams{
		ID: store.NewID(), OwnerID: owner, Kind: string(in.Kind), TargetType: string(in.TargetType),
		TargetID: target, Channel: string(in.Channel), Recipient: recipient,
		IdempotencyKey: strPtr(in.IdempotencyKey),
	}, nil
}

func parseTarget(t domain.TargetType, id string) (*uuid.UUID, error) {
	if !t.NeedsTargetID() {
		if id != "" {
			return nil, invalidField("target_id", "TARGET_ID_UNEXPECTED", "target type none takes no target id")
		}
		return nil, nil
	}
	parsed, err := parseID("target_id", id)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// parseRecipient requires an address for email and ignores one for other
// channels, which are copied by hand.
func parseRecipient(c domain.Channel, recipient string) (*string, error) {
	recipient = strings.TrimSpace(recipient)
	if c != domain.ChannelEmail {
		return nil, nil
	}
	addr, err := mail.ParseAddress(recipient)
	if err != nil || addr.Address != recipient {
		return nil, invalidField("recipient", "INVALID_RECIPIENT", "an email draft needs a plain email address as recipient")
	}
	return &recipient, nil
}

// Regenerate queues a new AI version of a pending draft. The draft stays
// pending and keeps showing its current version until the job finishes.
func (s *Service) Regenerate(ctx context.Context, draftID string, extraContext string, version int32) (db.Draft, error) {
	owner, id, err := ownerAndDraft(ctx, draftID, version)
	if err != nil {
		return db.Draft{}, err
	}
	if len(extraContext) > maxExtraContextLen {
		return db.Draft{}, invalidField("extra_context", "EXTRA_CONTEXT_TOO_LONG", "extra context is longer than %d bytes", maxExtraContextLen)
	}

	var res db.Draft
	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		d, getErr := repo.GetDraft(ctx, owner, id)
		if getErr != nil {
			return getErr
		}
		next, moveErr := toDomain(d).Regenerate(s.now())
		if moveErr != nil {
			return moveErr
		}
		saved, saveErr := saveState(ctx, repo, owner, d, next, version)
		if saveErr != nil {
			return saveErr
		}
		res = saved
		return s.enqueue(ctx, tx, GenerateArgs{DraftID: id, Version: saved.CurrentVersion + 1, ExtraContext: extraContext})
	})
	return res, err
}

// EditDraft stores the owner's text as a new version created by the user. A
// pending draft stays pending; an approved one returns to pending, so the
// edited text needs a fresh approval before anything leaves.
func (s *Service) EditDraft(ctx context.Context, draftID, subject, body string, version int32) (db.Draft, db.DraftVersion, error) {
	owner, id, err := ownerAndDraft(ctx, draftID, version)
	if err != nil {
		return db.Draft{}, db.DraftVersion{}, err
	}
	subject, body = strings.TrimSpace(subject), strings.TrimSpace(body)
	if err := validateText(subject, body); err != nil {
		return db.Draft{}, db.DraftVersion{}, err
	}

	var (
		res db.Draft
		ver db.DraftVersion
	)
	err = s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		d, getErr := repo.GetDraft(ctx, owner, id)
		if getErr != nil {
			return getErr
		}
		newVersion := d.CurrentVersion + 1
		next, moveErr := toDomain(d).NewVersion(newVersion, s.now())
		if moveErr != nil {
			return moveErr
		}
		saved, saveErr := saveState(ctx, repo, owner, d, next, version)
		if saveErr != nil {
			return saveErr
		}
		v, insErr := repo.InsertDraftVersion(ctx, db.InsertDraftVersionParams{
			DraftID: id, Version: newVersion, Subject: strPtr(subject), Body: body,
			BodySha256: hanko.BodyDigest(subject, body), CreatedBy: createdByUser,
		})
		res, ver = saved, v
		return insErr
	})
	return res, ver, err
}

func validateText(subject, body string) error {
	switch {
	case body == "":
		return invalidField("body", "BODY_REQUIRED", "body is required")
	case len(body) > maxBodyLen:
		return invalidField("body", "BODY_TOO_LONG", "body is longer than %d bytes", maxBodyLen)
	case len(subject) > maxSubjectLen:
		return invalidField("subject", "SUBJECT_TOO_LONG", "subject is longer than %d bytes", maxSubjectLen)
	}
	return nil
}

// Discard moves a pending draft to discarded.
func (s *Service) Discard(ctx context.Context, draftID string, version int32) (db.Draft, error) {
	owner, id, err := ownerAndDraft(ctx, draftID, version)
	if err != nil {
		return db.Draft{}, err
	}
	var res db.Draft
	err = s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		d, getErr := repo.GetDraft(ctx, owner, id)
		if getErr != nil {
			return getErr
		}
		next, moveErr := toDomain(d).Move(domain.DraftDiscarded, s.now())
		if moveErr != nil {
			return moveErr
		}
		res, getErr = saveState(ctx, repo, owner, d, next, version)
		return getErr
	})
	return res, err
}

// GetDraft returns a draft with all of its versions, newest first.
func (s *Service) GetDraft(ctx context.Context, draftID string) (DraftDetail, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return DraftDetail{}, err
	}
	id, err := parseID("draft_id", draftID)
	if err != nil {
		return DraftDetail{}, err
	}
	repo := store.New(s.pool)
	d, err := repo.GetDraft(ctx, owner, id)
	if err != nil {
		return DraftDetail{}, err
	}
	versions, err := repo.ListDraftVersions(ctx, id)
	return DraftDetail{Draft: d, Versions: versions}, err
}

// ListQueue returns one page of the owner's drafts in a state, newest first.
func (s *Service) ListQueue(ctx context.Context, in ListQueueInput) (ListQueueResult, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ListQueueResult{}, err
	}
	if in.State == "" {
		in.State = domain.DraftPending
	}
	if !in.State.Valid() {
		return ListQueueResult{}, invalidField("state", "INVALID_STATE", "unknown state %q", in.State)
	}
	drafts, next, err := store.New(s.pool).ListDrafts(ctx, owner, string(in.State), store.Page{Size: in.PageSize, Token: in.PageToken})
	return ListQueueResult{Drafts: drafts, NextPageToken: next}, err
}

// ownerAndDraft validates the arguments every versioned draft call shares.
func ownerAndDraft(ctx context.Context, draftID string, version int32) (owner, id uuid.UUID, err error) {
	if owner, err = ownerFrom(ctx); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if id, err = parseID("draft_id", draftID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if version < 1 {
		return uuid.Nil, uuid.Nil, invalidField("version", "VERSION_REQUIRED", "version is required")
	}
	return owner, id, nil
}
