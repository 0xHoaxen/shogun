package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store/db"
)

// NewPosting is what UpsertPosting stores.
type NewPosting struct {
	ID        uuid.UUID
	OwnerID   uuid.UUID
	SourceID  uuid.UUID
	Candidate domain.Candidate
	// Raw is the item as the source listed it, as JSON.
	Raw       []byte
	CreatedAt time.Time
}

// UpsertPosting stores a posting, or refreshes the one the source already
// listed under the same external id. It reports whether the posting is new.
func (r *Repo) UpsertPosting(ctx context.Context, n NewPosting) (db.UpsertPostingRow, error) {
	c := n.Candidate
	row, err := r.q.UpsertPosting(ctx, db.UpsertPostingParams{
		ID: n.ID, OwnerID: n.OwnerID, SourceID: n.SourceID, ExternalID: c.ExternalID, Title: c.Title,
		Company: textPtr(c.Company), Url: textPtr(c.URL), Location: textPtr(c.Location), PostedAt: c.PostedAt,
		Raw: n.Raw, CreatedAt: n.CreatedAt,
	})
	return row, wrap("upsert posting", err)
}

// GetPosting returns one of the owner's postings, or ErrNotFound.
func (r *Repo) GetPosting(ctx context.Context, owner, id uuid.UUID) (db.Posting, error) {
	row, err := r.q.GetPosting(ctx, db.GetPostingParams{OwnerID: owner, ID: id})
	return row, wrap("get posting", err)
}

// GetPostingByID returns a posting whatever its owner, for a job that already
// acts for the owner it carries.
func (r *Repo) GetPostingByID(ctx context.Context, id uuid.UUID) (db.Posting, error) {
	row, err := r.q.GetPostingByID(ctx, id)
	return row, wrap("get posting", err)
}

// PostingRow is a posting with its score, when it has one.
type PostingRow = db.ListPostingsRow

// PostingFilter narrows a list; a nil field matches any.
type PostingFilter struct {
	// MinScore keeps only scored postings at or above it.
	MinScore *float32
	SourceID *uuid.UUID
}

// ListPostings returns one page of the owner's postings, best score first and
// unscored last, and the token for the next page (empty on the last page).
func (r *Repo) ListPostings(ctx context.Context, owner uuid.UUID, f PostingFilter, page Page) ([]db.ListPostingsRow, string, error) {
	c, err := decodeCursor(page.Token)
	if err != nil {
		return nil, "", err
	}
	size := page.size()
	arg := db.ListPostingsParams{OwnerID: owner, MinScore: f.MinScore, SourceID: f.SourceID, RowLimit: size + 1}
	if c != nil {
		arg.AfterID, arg.AfterScore = &c.ID, &c.Score
	}
	rows, err := r.q.ListPostings(ctx, arg)
	if err != nil {
		return nil, "", wrap("list postings", err)
	}
	if int32(len(rows)) <= size {
		return rows, "", nil
	}
	rows = rows[:size]
	last := rows[size-1]
	score := unscored
	if last.Score != nil {
		score = *last.Score
	}
	return rows, cursor{Score: score, ID: last.ID}.encode(), nil
}

// UnscoredPostingIDs returns the source's postings that have no score yet.
func (r *Repo) UnscoredPostingIDs(ctx context.Context, source uuid.UUID) ([]uuid.UUID, error) {
	ids, err := r.q.ListUnscoredPostingIDs(ctx, source)
	return ids, wrap("list unscored postings", err)
}

// SetSavedJob records the kagami job a posting was saved as. It reports false,
// and returns the posting as it is, when the posting was already saved.
func (r *Repo) SetSavedJob(ctx context.Context, owner, id, jobID uuid.UUID) (db.Posting, bool, error) {
	row, err := r.q.SetSavedJob(ctx, db.SetSavedJobParams{OwnerID: owner, ID: id, SavedJobID: &jobID})
	if err == nil {
		return row, true, nil
	}
	existing, getErr := r.GetPosting(ctx, owner, id)
	if getErr != nil {
		return db.Posting{}, false, getErr
	}
	if !errors.Is(mapErr(err), ErrNotFound) {
		return db.Posting{}, false, wrap("set saved job", err)
	}
	return existing, false, nil
}

// SaveScore stores a posting's score, replacing an earlier one: a model's score
// replaces the rule's.
func (r *Repo) SaveScore(ctx context.Context, posting uuid.UUID, s domain.Score, scoredBy string, at time.Time) error {
	reasons, err := json.Marshal(nonNil(s.Reasons))
	if err != nil {
		return fmt.Errorf("store: marshal reasons: %w", err)
	}
	return wrap("save score", r.q.UpsertScore(ctx, db.UpsertScoreParams{
		PostingID: posting, Score: s.Value, Reasons: reasons, ScoredBy: scoredBy, CreatedAt: at,
	}))
}

// MarkMatched records that a posting has reached the owner's minimum. It
// reports true only the first time, which is when the match is announced.
func (r *Repo) MarkMatched(ctx context.Context, posting uuid.UUID, at time.Time) (bool, error) {
	_, err := r.q.MarkMatched(ctx, db.MarkMatchedParams{PostingID: posting, MatchedAt: &at})
	if errors.Is(mapErr(err), ErrNotFound) {
		return false, nil
	}
	return err == nil, wrap("mark matched", err)
}

// GetScore returns a posting's score, or ErrNotFound when it has none.
func (r *Repo) GetScore(ctx context.Context, posting uuid.UUID) (db.Score, error) {
	row, err := r.q.GetScore(ctx, posting)
	return row, wrap("get score", err)
}

// ReasonsOf reads the reasons stored with a score.
func ReasonsOf(raw []byte) []string {
	var reasons []string
	// Written by this service as a list of strings; unreadable means none.
	_ = json.Unmarshal(raw, &reasons)
	return reasons
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
