package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/fetch"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store/db"
)

// Failure codes kept with a source as its last error.
const (
	CodeRobotsDisallowed = "robots_disallowed"
	CodeAddressNotPublic = "address_not_public"
	CodeTooLarge         = "too_large"
	CodeBadDocument      = "bad_document"
	CodeFetchFailed      = "fetch_failed"
	CodeInternal         = "internal"
)

// RunResult says what reading a source did.
type RunResult struct {
	// Fetched is how many postings the source listed, Added how many were new,
	// and Skipped how many could not be read as postings.
	Fetched, Added, Skipped int
}

// RunSource reads one of the calling owner's sources now.
func (s *Service) RunSource(ctx context.Context, id uuid.UUID) (RunResult, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return RunResult{}, err
	}
	return s.RunSourceFor(ctx, owner, id)
}

// RunSourceFor reads a source for its owner, which is what the schedule runs. It
// records when the source was read and how it went; a source that could not be
// read returns a *RunError.
func (s *Service) RunSourceFor(ctx context.Context, owner, id uuid.UUID) (RunResult, error) {
	repo := store.New(s.pool)
	src, err := repo.GetSource(ctx, owner, id)
	if errors.Is(err, store.ErrNotFound) {
		return RunResult{}, ErrSourceNotFound
	}
	if err != nil {
		return RunResult{}, err
	}
	res, runErr := s.read(ctx, owner, src)
	code := ""
	if runErr != nil {
		code = failureCode(runErr)
	}
	if markErr := repo.MarkSourceRun(ctx, id, s.now().UTC(), code); markErr != nil {
		if runErr == nil {
			return res, markErr
		}
		s.log.Warn("could not record a failed source run", "source_id", id.String())
	}
	if runErr != nil {
		if code == CodeInternal {
			return res, runErr
		}
		return res, &RunError{Code: code, Err: runErr}
	}
	return res, nil
}

// read fetches a source, parses it and stores its postings.
func (s *Service) read(ctx context.Context, owner uuid.UUID, src db.Source) (RunResult, error) {
	cfg, err := store.ConfigOf(src)
	if err != nil {
		return RunResult{}, err
	}
	items, err := s.items(ctx, domain.SourceKind(src.Kind), cfg)
	if err != nil {
		return RunResult{}, err
	}
	res := RunResult{Fetched: len(items)}
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		repo := store.New(tx)
		for _, item := range items {
			candidate, err := item.Candidate.Validate()
			if err != nil {
				res.Skipped++
				continue
			}
			row, err := repo.UpsertPosting(ctx, store.NewPosting{
				ID: store.NewID(), OwnerID: owner, SourceID: src.ID, Candidate: candidate, Raw: item.Raw, CreatedAt: s.now().UTC(),
			})
			if err != nil {
				return err
			}
			if row.Inserted {
				res.Added++
			}
		}
		return nil
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("store postings: %w", err)
	}
	return res, nil
}

func (s *Service) items(ctx context.Context, kind domain.SourceKind, cfg domain.SourceConfig) ([]fetch.Item, error) {
	switch kind {
	case domain.KindRSS:
		doc, err := s.fetcher.Get(ctx, cfg.URL)
		if err != nil {
			return nil, err
		}
		return fetch.ParseFeed(doc)
	case domain.KindAPI:
		doc, err := s.fetcher.Get(ctx, cfg.URL)
		if err != nil {
			return nil, err
		}
		return fetch.ParseJSON(doc, mappingOf(cfg))
	case domain.KindFile:
		return fetch.ParseJSON([]byte(cfg.Document), mappingOf(cfg))
	default:
		return nil, fmt.Errorf("%w: unknown kind %q", fetch.ErrBadDocument, kind)
	}
}

func mappingOf(cfg domain.SourceConfig) domain.FieldMapping {
	if cfg.Mapping == nil {
		return domain.FieldMapping{}
	}
	return *cfg.Mapping
}

// failureCode names why a source could not be read.
func failureCode(err error) string {
	switch {
	case errors.Is(err, fetch.ErrRobotsDisallowed):
		return CodeRobotsDisallowed
	case errors.Is(err, fetch.ErrNotPublic):
		return CodeAddressNotPublic
	case errors.Is(err, fetch.ErrTooLarge):
		return CodeTooLarge
	case errors.Is(err, fetch.ErrBadDocument):
		return CodeBadDocument
	case errors.Is(err, fetch.ErrFailed), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return CodeFetchFailed
	default:
		return CodeInternal
	}
}

// DueRun is a source that is due to be read.
type DueRun struct {
	OwnerID  uuid.UUID
	SourceID uuid.UUID
	// Slot names the schedule slot that makes it due, so one slot is run once.
	Slot string
}

// DueSources returns the enabled sources that are due at now, read on their
// schedules in loc. A source with an unreadable schedule is skipped and logged.
func (s *Service) DueSources(ctx context.Context, now time.Time, loc *time.Location) ([]DueRun, error) {
	sources, err := store.New(s.pool).EnabledSources(ctx)
	if err != nil {
		return nil, err
	}
	var due []DueRun
	for _, src := range sources {
		slot, isDue, err := domain.Due(src.Schedule, src.LastRunAt, now, loc)
		if err != nil {
			s.log.Warn("source has an unreadable schedule", "source_id", src.ID.String())
			continue
		}
		if isDue {
			due = append(due, DueRun{OwnerID: src.OwnerID, SourceID: src.ID, Slot: slot})
		}
	}
	return due, nil
}
