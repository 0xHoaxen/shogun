package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
)

// NewSnapshot is what InsertSnapshot stores. Repos and Contributions are JSON.
type NewSnapshot struct {
	ID            uuid.UUID
	OwnerID       uuid.UUID
	TakenAt       time.Time
	Repos         []byte
	Contributions []byte
	// ETag is GitHub's validator for the data, sent back on the next sync.
	ETag string
}

// InsertSnapshot stores a GitHub snapshot.
func (r *Repo) InsertSnapshot(ctx context.Context, n NewSnapshot) (db.GithubSnapshot, error) {
	row, err := r.q.InsertSnapshot(ctx, db.InsertSnapshotParams{
		ID: n.ID, OwnerID: n.OwnerID, TakenAt: n.TakenAt, Repos: n.Repos, Contributions: n.Contributions, Etag: textPtr(n.ETag),
	})
	return row, wrap("insert snapshot", err)
}

// LatestSnapshots returns up to limit of the owner's newest snapshots, newest
// first.
func (r *Repo) LatestSnapshots(ctx context.Context, owner uuid.UUID, limit int32) ([]db.GithubSnapshot, error) {
	rows, err := r.q.ListLatestSnapshots(ctx, db.ListLatestSnapshotsParams{OwnerID: owner, RowLimit: limit})
	return rows, wrap("list snapshots", err)
}

// textPtr stores empty text as NULL.
func textPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// SnapshotOwners returns every owner with at least one snapshot.
func (r *Repo) SnapshotOwners(ctx context.Context) ([]uuid.UUID, error) {
	ids, err := r.q.ListSnapshotOwners(ctx)
	return ids, wrap("list snapshot owners", err)
}
