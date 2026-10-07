package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store/db"
)

// NewSource is what InsertSource stores.
type NewSource struct {
	ID      uuid.UUID
	OwnerID uuid.UUID
	Input   domain.SourceInput
}

// InsertSource stores a source.
func (r *Repo) InsertSource(ctx context.Context, n NewSource) (db.Source, error) {
	config, err := json.Marshal(n.Input.Config)
	if err != nil {
		return db.Source{}, fmt.Errorf("store: marshal source config: %w", err)
	}
	row, err := r.q.InsertSource(ctx, db.InsertSourceParams{
		ID: n.ID, OwnerID: n.OwnerID, Name: n.Input.Name, Kind: string(n.Input.Kind), Config: config,
		Schedule: n.Input.Schedule, Enabled: n.Input.Enabled,
	})
	return row, wrap("insert source", err)
}

// GetSource returns one of the owner's sources, or ErrNotFound.
func (r *Repo) GetSource(ctx context.Context, owner, id uuid.UUID) (db.Source, error) {
	row, err := r.q.GetSource(ctx, db.GetSourceParams{OwnerID: owner, ID: id})
	return row, wrap("get source", err)
}

// ListSources returns the owner's sources by name.
func (r *Repo) ListSources(ctx context.Context, owner uuid.UUID) ([]db.Source, error) {
	rows, err := r.q.ListSources(ctx, owner)
	return rows, wrap("list sources", err)
}

// UpdateSource writes a source's name, config, schedule and enabled flag. Its
// kind never changes. It returns ErrNotFound when the owner has no such source.
func (r *Repo) UpdateSource(ctx context.Context, owner, id uuid.UUID, in domain.SourceInput) (db.Source, error) {
	config, err := json.Marshal(in.Config)
	if err != nil {
		return db.Source{}, fmt.Errorf("store: marshal source config: %w", err)
	}
	row, err := r.q.UpdateSource(ctx, db.UpdateSourceParams{
		OwnerID: owner, ID: id, Name: in.Name, Config: config, Schedule: in.Schedule, Enabled: in.Enabled,
	})
	return row, wrap("update source", err)
}

// EnabledSources returns every owner's enabled sources, for the scheduler.
func (r *Repo) EnabledSources(ctx context.Context) ([]db.Source, error) {
	rows, err := r.q.ListEnabledSources(ctx)
	return rows, wrap("list enabled sources", err)
}

// MarkSourceRun records when a source was last read and how it went. failure is
// a short code, or empty on success.
func (r *Repo) MarkSourceRun(ctx context.Context, id uuid.UUID, at time.Time, failure string) error {
	return wrap("mark source run", r.q.MarkSourceRun(ctx, db.MarkSourceRunParams{
		ID: id, LastRunAt: &at, LastError: textPtr(failure),
	}))
}

// ConfigOf reads a stored source's config.
func ConfigOf(s db.Source) (domain.SourceConfig, error) {
	var cfg domain.SourceConfig
	if err := json.Unmarshal(s.Config, &cfg); err != nil {
		return cfg, fmt.Errorf("store: read config of source %s: %w", s.ID, err)
	}
	return cfg, nil
}

// textPtr stores empty text as NULL.
func textPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
