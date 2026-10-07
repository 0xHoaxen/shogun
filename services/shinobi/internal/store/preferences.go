package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store/db"
)

// GetPreferences returns the owner's preferences, or the defaults when they
// have set none.
func (r *Repo) GetPreferences(ctx context.Context, owner uuid.UUID) (domain.Preferences, error) {
	row, err := r.q.GetPreferences(ctx, owner)
	if errors.Is(mapErr(err), ErrNotFound) {
		return domain.DefaultPreferences(), nil
	}
	if err != nil {
		return domain.Preferences{}, wrap("get preferences", err)
	}
	return preferencesOf(row), nil
}

// SetPreferences stores the owner's preferences, replacing any set before.
func (r *Repo) SetPreferences(ctx context.Context, owner uuid.UUID, p domain.Preferences, at time.Time) (domain.Preferences, error) {
	row, err := r.q.UpsertPreferences(ctx, db.UpsertPreferencesParams{
		OwnerID: owner, Roles: nonNil(p.Roles), Locations: nonNil(p.Locations), MustHave: nonNil(p.MustHave),
		NiceToHave: nonNil(p.NiceToHave), Exclude: nonNil(p.Exclude), MinScore: p.MinScore, UpdatedAt: at,
	})
	if err != nil {
		return domain.Preferences{}, wrap("set preferences", err)
	}
	return preferencesOf(row), nil
}

func preferencesOf(row db.Preference) domain.Preferences {
	return domain.Preferences{
		Roles: row.Roles, Locations: row.Locations, MustHave: row.MustHave, NiceToHave: row.NiceToHave,
		Exclude: row.Exclude, MinScore: row.MinScore,
	}
}
