package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
)

// Errors the transport maps to gRPC status codes.
var (
	// ErrSuggestionNotFound means the owner has no such suggestion.
	ErrSuggestionNotFound = errors.New("app: suggestion not found")
	// ErrAlreadyDecided means the suggestion was accepted or dismissed before.
	ErrAlreadyDecided = errors.New("app: suggestion already decided")
)

// SuggestionPage is one page of suggestions.
type SuggestionPage struct {
	Suggestions   []db.Suggestion
	NextPageToken string
}

// ListSuggestions returns one page of the calling owner's suggestions, newest
// first. A nil filter field matches any.
func (s *Service) ListSuggestions(ctx context.Context, f store.SuggestionFilter, page store.Page) (SuggestionPage, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return SuggestionPage{}, err
	}
	rows, next, err := store.New(s.pool).ListSuggestions(ctx, owner, f, page)
	if err != nil {
		return SuggestionPage{}, wrap("list suggestions", err)
	}
	return SuggestionPage{Suggestions: rows, NextPageToken: next}, nil
}

// AcceptSuggestion records that the owner took an open suggestion. It changes
// nothing else: katana never edits a resume or a profile.
func (s *Service) AcceptSuggestion(ctx context.Context, id uuid.UUID) (db.Suggestion, error) {
	return s.decide(ctx, id, domain.StateAccepted)
}

// DismissSuggestion records that the owner turned an open suggestion down.
func (s *Service) DismissSuggestion(ctx context.Context, id uuid.UUID) (db.Suggestion, error) {
	return s.decide(ctx, id, domain.StateDismissed)
}

func (s *Service) decide(ctx context.Context, id uuid.UUID, to domain.State) (db.Suggestion, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Suggestion{}, err
	}
	// The state machine says what is allowed; the store makes it happen once.
	if _, err := domain.StateOpen.Decide(to); err != nil {
		return db.Suggestion{}, err
	}
	row, err := store.New(s.pool).Decide(ctx, owner, id, to, s.now().UTC())
	switch {
	case errors.Is(err, store.ErrNotFound):
		return db.Suggestion{}, ErrSuggestionNotFound
	case errors.Is(err, store.ErrNotOpen):
		return db.Suggestion{}, &domain.DecisionError{Reason: domain.ReasonSuggestionAlreadyDecided, To: to}
	case err != nil:
		return db.Suggestion{}, wrap("decide suggestion", err)
	}
	return row, nil
}
