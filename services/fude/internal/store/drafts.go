package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
)

// InsertDraft creates a draft. A repeated idempotency key is ErrDuplicate.
func (r *Repo) InsertDraft(ctx context.Context, arg db.InsertDraftParams) (db.Draft, error) {
	d, err := r.q.InsertDraft(ctx, arg)
	if err != nil {
		return db.Draft{}, fmt.Errorf("insert draft: %w", mapErr(err))
	}
	return d, nil
}

// GetDraft returns one draft of the owner.
func (r *Repo) GetDraft(ctx context.Context, owner, id uuid.UUID) (db.Draft, error) {
	d, err := r.q.GetDraft(ctx, db.GetDraftParams{ID: id, OwnerID: owner})
	return d, mapErr(err)
}

// GetDraftByIdempotencyKey returns the draft a retried GenerateDraft created
// earlier.
func (r *Repo) GetDraftByIdempotencyKey(ctx context.Context, owner uuid.UUID, key string) (db.Draft, error) {
	d, err := r.q.GetDraftByIdempotencyKey(ctx, db.GetDraftByIdempotencyKeyParams{OwnerID: owner, IdempotencyKey: key})
	return d, mapErr(err)
}

// ListDrafts returns one page of the owner's drafts in state, most recently
// updated first, and the token for the next page, empty on the last page.
func (r *Repo) ListDrafts(ctx context.Context, owner uuid.UUID, state string, p Page) ([]db.Draft, string, error) {
	after, err := decodeCursor(p.Token)
	if err != nil {
		return nil, "", err
	}
	size := p.size()
	afterAt, afterID := afterParams(after)
	rows, err := r.q.ListDrafts(ctx, db.ListDraftsParams{
		OwnerID: owner, State: state, AfterUpdatedAt: afterAt, AfterID: afterID, RowLimit: size + 1,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list drafts: %w", mapErr(err))
	}
	rows, next := trimPage(rows, size, func(d db.Draft) (time.Time, uuid.UUID) { return d.UpdatedAt, d.ID })
	return rows, next, nil
}

// UpdateDraftState writes the state columns when arg.Version is current. A
// stale version is ErrVersionConflict.
func (r *Repo) UpdateDraftState(ctx context.Context, arg db.UpdateDraftStateParams) (db.Draft, error) {
	d, err := r.q.UpdateDraftState(ctx, arg)
	if err != nil {
		return db.Draft{}, staleOrMissing(err, func() error {
			_, getErr := r.q.GetDraft(ctx, db.GetDraftParams{ID: arg.ID, OwnerID: arg.OwnerID})
			return getErr
		})
	}
	return d, nil
}

// InsertDraftVersion appends a version to a draft.
func (r *Repo) InsertDraftVersion(ctx context.Context, arg db.InsertDraftVersionParams) (db.DraftVersion, error) {
	v, err := r.q.InsertDraftVersion(ctx, arg)
	if err != nil {
		return db.DraftVersion{}, fmt.Errorf("insert draft version: %w", mapErr(err))
	}
	return v, nil
}

// ListDraftVersions returns a draft's versions, newest first.
func (r *Repo) ListDraftVersions(ctx context.Context, draftID uuid.UUID) ([]db.DraftVersion, error) {
	vs, err := r.q.ListDraftVersions(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("list draft versions: %w", mapErr(err))
	}
	return vs, nil
}

// InsertVoiceSample stores a sample of the owner's writing. Its embedding is
// filled in later by the embed_voice_sample job.
func (r *Repo) InsertVoiceSample(ctx context.Context, arg db.InsertVoiceSampleParams) (db.InsertVoiceSampleRow, error) {
	v, err := r.q.InsertVoiceSample(ctx, arg)
	if err != nil {
		return db.InsertVoiceSampleRow{}, fmt.Errorf("insert voice sample: %w", mapErr(err))
	}
	return v, nil
}

// ListRecentVoiceSamples returns up to limit of the owner's newest samples for
// a channel.
func (r *Repo) ListRecentVoiceSamples(ctx context.Context, owner uuid.UUID, channel string, limit int32) ([]db.ListRecentVoiceSamplesRow, error) {
	rows, err := r.q.ListRecentVoiceSamples(ctx, db.ListRecentVoiceSamplesParams{OwnerID: owner, Channel: channel, RowLimit: limit})
	if err != nil {
		return nil, fmt.Errorf("list voice samples: %w", mapErr(err))
	}
	return rows, nil
}

// GetTemplate returns the owner's template for a kind and channel, tied to a
// contact status when contactStatus is not nil. It is ErrNotFound when the
// owner has not written one.
func (r *Repo) GetTemplate(ctx context.Context, owner uuid.UUID, kind, channel string, contactStatus *string) (db.Template, error) {
	t, err := r.q.GetTemplate(ctx, db.GetTemplateParams{OwnerID: owner, Kind: kind, Channel: channel, ContactStatus: contactStatus})
	return t, mapErr(err)
}

// SetDraftRecipient gives a draft without a recipient one. It reports whether
// a recipient was set.
func (r *Repo) SetDraftRecipient(ctx context.Context, owner, id uuid.UUID, recipient string) (bool, error) {
	n, err := r.q.SetDraftRecipient(ctx, db.SetDraftRecipientParams{ID: id, OwnerID: owner, Recipient: &recipient})
	if err != nil {
		return false, fmt.Errorf("set draft recipient: %w", mapErr(err))
	}
	return n > 0, nil
}
