package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/katana/internal/store"
)

// SyncResult says what a sync did.
type SyncResult struct {
	// Changed is false when GitHub had nothing new, so no snapshot was stored.
	Changed    bool
	SnapshotID uuid.UUID
}

// SyncGitHub reads GitHub for the calling owner.
func (s *Service) SyncGitHub(ctx context.Context) (SyncResult, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	return s.syncOwner(ctx, owner)
}

// SyncAll reads GitHub for every owner who has synced before, which is what the
// daily job runs. One owner failing does not stop the others; the failures come
// back joined so the job is retried.
func (s *Service) SyncAll(ctx context.Context, log *slog.Logger) error {
	if s.github == nil {
		return ErrGitHubNotConfigured
	}
	owners, err := store.New(s.pool).SnapshotOwners(ctx)
	if err != nil {
		return fmt.Errorf("list owners: %w", err)
	}
	var errs []error
	for _, owner := range owners {
		res, err := s.syncOwner(ctx, owner)
		if err != nil {
			errs = append(errs, fmt.Errorf("sync owner %s: %w", owner, err))
			continue
		}
		log.Info("github sync done", slog.String("owner_id", owner.String()), slog.Bool("changed", res.Changed))
	}
	return errors.Join(errs...)
}

func (s *Service) syncOwner(ctx context.Context, owner uuid.UUID) (SyncResult, error) {
	if s.github == nil {
		return SyncResult{}, ErrGitHubNotConfigured
	}
	repo := store.New(s.pool)
	latest, err := repo.LatestSnapshots(ctx, owner, 1)
	if err != nil {
		return SyncResult{}, err
	}
	etag := ""
	if len(latest) > 0 && latest[0].Etag != nil {
		etag = *latest[0].Etag
	}
	snap, err := s.github.Fetch(ctx, etag)
	if errors.Is(err, errNotModified) {
		return SyncResult{}, nil
	}
	if err != nil {
		return SyncResult{}, fmt.Errorf("read github: %w", err)
	}
	repos, err := json.Marshal(snap.Repos)
	if err != nil {
		return SyncResult{}, fmt.Errorf("marshal repos: %w", err)
	}
	contributions, err := json.Marshal(snap.Contributions)
	if err != nil {
		return SyncResult{}, fmt.Errorf("marshal contributions: %w", err)
	}
	row, err := repo.InsertSnapshot(ctx, store.NewSnapshot{
		ID: store.NewID(), OwnerID: owner, TakenAt: s.now().UTC(), Repos: repos, Contributions: contributions, ETag: snap.ETag,
	})
	if err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Changed: true, SnapshotID: row.ID}, nil
}
