package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store/db"
)

// MaxSources is how many sources one owner may have.
const MaxSources = 20

// UpsertSource creates a source, or updates the one named by id. An update may
// not change the source's kind.
func (s *Service) UpsertSource(ctx context.Context, id *uuid.UUID, in domain.SourceInput) (db.Source, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Source{}, err
	}
	clean, err := in.Validate()
	if err != nil {
		return db.Source{}, err
	}
	repo := store.New(s.pool)
	if id == nil {
		existing, err := repo.ListSources(ctx, owner)
		if err != nil {
			return db.Source{}, err
		}
		if len(existing) >= MaxSources {
			return db.Source{}, ErrTooManySources
		}
		row, err := repo.InsertSource(ctx, store.NewSource{ID: store.NewID(), OwnerID: owner, Input: clean})
		return row, wrap("add source", err)
	}
	current, err := repo.GetSource(ctx, owner, *id)
	if errors.Is(err, store.ErrNotFound) {
		return db.Source{}, ErrSourceNotFound
	}
	if err != nil {
		return db.Source{}, err
	}
	if current.Kind != string(clean.Kind) {
		return db.Source{}, ErrKindFixed
	}
	row, err := repo.UpdateSource(ctx, owner, *id, clean)
	if errors.Is(err, store.ErrNotFound) {
		return db.Source{}, ErrSourceNotFound
	}
	return row, wrap("update source", err)
}

// ListSources returns the calling owner's sources.
func (s *Service) ListSources(ctx context.Context) ([]db.Source, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(s.pool).ListSources(ctx, owner)
	return rows, wrap("list sources", err)
}
