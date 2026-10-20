package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/pkg/owners"
	"github.com/0xHoaxen/shogun/pkg/postgres"

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

// SyncFor reads GitHub for one owner, which is what the daily job runs for each
// owner on their own clock.
func (s *Service) SyncFor(ctx context.Context, owner uuid.UUID) (SyncResult, error) {
	return s.syncOwner(ctx, owner)
}

// SyncableOwners lists the active owners whose GitHub the daily job may read:
// those who have connected a GitHub account of their own.
func (s *Service) SyncableOwners(ctx context.Context) ([]owners.Owner, error) {
	if s.github == nil {
		return nil, nil
	}
	active, err := owners.ListActive(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	connected, err := store.New(s.pool).GitHubAccountOwners(ctx)
	if err != nil {
		return nil, fmt.Errorf("list connected owners: %w", err)
	}
	has := make(map[uuid.UUID]struct{}, len(connected))
	for _, id := range connected {
		has[id] = struct{}{}
	}
	var out []owners.Owner
	for _, o := range active {
		if _, ok := has[o.ID]; ok {
			out = append(out, o)
		}
	}
	return out, nil
}

func (s *Service) syncOwner(ctx context.Context, owner uuid.UUID) (SyncResult, error) {
	if s.github == nil {
		return SyncResult{}, ErrGitHubNotConfigured
	}
	repo := store.New(s.pool)
	creds, err := s.credentialsOf(ctx, repo, owner)
	if err != nil {
		return SyncResult{}, err
	}
	latest, err := repo.LatestSnapshots(ctx, owner, 1)
	if err != nil {
		return SyncResult{}, err
	}
	etag := ""
	if len(latest) > 0 && latest[0].Etag != nil {
		etag = *latest[0].Etag
	}
	snap, err := s.github.Fetch(ctx, creds, etag)
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
	var res SyncResult
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		row, err := store.New(tx).InsertSnapshot(ctx, store.NewSnapshot{
			ID: store.NewID(), OwnerID: owner, TakenAt: s.now().UTC(), Repos: repos, Contributions: contributions, ETag: snap.ETag,
		})
		if err != nil {
			return err
		}
		res = SyncResult{Changed: true, SnapshotID: row.ID}
		if s.queue == nil {
			return nil
		}
		// The snapshot and the run it calls for succeed or fail together.
		return s.queue.EnqueueSuggest(ctx, tx, SuggestInput{OwnerID: owner, SnapshotID: &row.ID})
	})
	if err != nil {
		return SyncResult{}, err
	}
	return res, nil
}
