package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
)

// ErrNotOpen means a decision found the suggestion already decided.
var ErrNotOpen = errors.New("store: suggestion is not open")

// NewSuggestion is what InsertSuggestion stores.
type NewSuggestion struct {
	ID         uuid.UUID
	OwnerID    uuid.UUID
	Suggestion domain.Suggestion
	CreatedAt  time.Time
}

// InsertSuggestion stores an open suggestion.
func (r *Repo) InsertSuggestion(ctx context.Context, n NewSuggestion) (db.Suggestion, error) {
	evidence := n.Suggestion.Evidence
	if evidence == nil {
		evidence = []domain.Evidence{}
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return db.Suggestion{}, fmt.Errorf("store: marshal evidence: %w", err)
	}
	row, err := r.q.InsertSuggestion(ctx, db.InsertSuggestionParams{
		ID: n.ID, OwnerID: n.OwnerID, Target: string(n.Suggestion.Target), Section: string(n.Suggestion.Section),
		Before: textPtr(n.Suggestion.Before), After: n.Suggestion.After, Reason: n.Suggestion.Reason,
		Evidence: raw, CreatedAt: n.CreatedAt,
	})
	return row, wrap("insert suggestion", err)
}

// GetSuggestion returns one of the owner's suggestions, or ErrNotFound.
func (r *Repo) GetSuggestion(ctx context.Context, owner, id uuid.UUID) (db.Suggestion, error) {
	row, err := r.q.GetSuggestion(ctx, db.GetSuggestionParams{OwnerID: owner, ID: id})
	return row, wrap("get suggestion", err)
}

// SuggestionFilter narrows a list; a nil field matches any.
type SuggestionFilter struct {
	State  *domain.State
	Target *domain.Target
}

// ListSuggestions returns one page of the owner's suggestions, newest first,
// and the token for the next page (empty on the last page).
func (r *Repo) ListSuggestions(ctx context.Context, owner uuid.UUID, f SuggestionFilter, page Page) ([]db.Suggestion, string, error) {
	c, err := decodeCursor(page.Token)
	if err != nil {
		return nil, "", err
	}
	size := page.size()
	afterAt, afterID := afterParams(c)
	var state, target *string
	if f.State != nil {
		s := string(*f.State)
		state = &s
	}
	if f.Target != nil {
		t := string(*f.Target)
		target = &t
	}
	rows, err := r.q.ListSuggestions(ctx, db.ListSuggestionsParams{
		OwnerID: owner, State: state, Target: target, AfterCreatedAt: afterAt, AfterID: afterID, RowLimit: size + 1,
	})
	if err != nil {
		return nil, "", wrap("list suggestions", err)
	}
	if int32(len(rows)) <= size {
		return rows, "", nil
	}
	rows = rows[:size]
	last := rows[size-1]
	return rows, cursor{CreatedAt: last.CreatedAt, ID: last.ID}.encode(), nil
}

// Decide moves an open suggestion to state at decidedAt. It returns ErrNotOpen
// when the suggestion was decided already and ErrNotFound when it does not
// exist.
func (r *Repo) Decide(ctx context.Context, owner, id uuid.UUID, state domain.State, decidedAt time.Time) (db.Suggestion, error) {
	row, err := r.q.DecideSuggestion(ctx, db.DecideSuggestionParams{
		OwnerID: owner, ID: id, State: string(state), DecidedAt: &decidedAt,
	})
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Suggestion{}, wrap("decide suggestion", err)
	}
	if _, getErr := r.q.GetSuggestion(ctx, db.GetSuggestionParams{OwnerID: owner, ID: id}); getErr != nil {
		return db.Suggestion{}, wrap("decide suggestion", getErr)
	}
	return db.Suggestion{}, ErrNotOpen
}
