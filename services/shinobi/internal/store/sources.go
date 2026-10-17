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

// DeleteSource removes one of the owner's sources with its postings and their
// scores. It returns ErrNotFound when the owner has no such source.
func (r *Repo) DeleteSource(ctx context.Context, owner, id uuid.UUID) error {
	_, err := r.q.DeleteSource(ctx, db.DeleteSourceParams{OwnerID: owner, ID: id})
	return wrap("delete source", err)
}

// ScheduledSource is an enabled source with the time zone of its owner, which
// its schedule is read in.
type ScheduledSource struct {
	db.Source
	// Timezone is an IANA name.
	Timezone string
}

// EnabledSources returns the enabled sources of every active owner, for the
// scheduler. Sources of a disabled or deleted owner, or of an owner torii has
// not announced yet, are not included.
func (r *Repo) EnabledSources(ctx context.Context) ([]ScheduledSource, error) {
	rows, err := r.q.ListEnabledSources(ctx)
	if err != nil {
		return nil, wrap("list enabled sources", err)
	}
	out := make([]ScheduledSource, len(rows))
	for i, row := range rows {
		out[i] = ScheduledSource{
			Source: db.Source{
				ID: row.ID, OwnerID: row.OwnerID, Name: row.Name, Kind: row.Kind, Config: row.Config,
				Schedule: row.Schedule, Enabled: row.Enabled, LastRunAt: row.LastRunAt, LastError: row.LastError,
			},
			Timezone: row.OwnerTimezone,
		}
	}
	return out, nil
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
