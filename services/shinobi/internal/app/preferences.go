package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
)

// maxRescored is how many of the owner's postings a change of preferences
// scores again.
const maxRescored = 1000

// GetPreferences returns the calling owner's preferences, or the defaults.
func (s *Service) GetPreferences(ctx context.Context) (domain.Preferences, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return domain.Preferences{}, err
	}
	p, err := store.New(s.pool).GetPreferences(ctx, owner)
	return p, wrap("get preferences", err)
}

// SetPreferences stores the calling owner's preferences and, in the same
// transaction, queues a new score for each of their postings, since what
// matched before may not now.
func (s *Service) SetPreferences(ctx context.Context, in domain.Preferences) (domain.Preferences, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return domain.Preferences{}, err
	}
	clean, err := in.Validate()
	if err != nil {
		return domain.Preferences{}, err
	}
	at := s.now().UTC()
	var saved domain.Preferences
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		repo := store.New(tx)
		saved, err = repo.SetPreferences(ctx, owner, clean, at)
		if err != nil || s.queue == nil {
			return err
		}
		rows, _, err := repo.ListPostings(ctx, owner, store.PostingFilter{}, store.Page{Size: maxRescored})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := s.queue.EnqueueScore(ctx, tx, ScoreInput{OwnerID: owner, PostingID: row.ID, Version: at.UnixMicro()}); err != nil {
				return err
			}
		}
		return nil
	})
	return saved, wrap("set preferences", err)
}

// PostingPage is one page of postings.
type PostingPage struct {
	Postings      []store.PostingRow
	NextPageToken string
}

// ListPostings returns one page of the calling owner's postings, best score
// first and unscored last.
func (s *Service) ListPostings(ctx context.Context, f store.PostingFilter, page store.Page) (PostingPage, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return PostingPage{}, err
	}
	rows, next, err := store.New(s.pool).ListPostings(ctx, owner, f, page)
	if err != nil {
		return PostingPage{}, wrap("list postings", err)
	}
	return PostingPage{Postings: rows, NextPageToken: next}, nil
}
