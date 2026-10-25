package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
)

// GetProfile returns the owner's profile, or ErrNotFound.
func (r *Repo) GetProfile(ctx context.Context, owner uuid.UUID) (db.Profile, error) {
	row, err := r.q.GetProfile(ctx, owner)
	return row, wrap("get profile", err)
}

// InsertProfile creates the owner's profile at version 1. It is
// ErrVersionConflict when they already have one.
func (r *Repo) InsertProfile(ctx context.Context, arg db.InsertProfileParams) (db.Profile, error) {
	row, err := r.q.InsertProfile(ctx, arg)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Profile{}, ErrVersionConflict
	}
	return row, wrap("insert profile", err)
}

// UpdateProfile changes the owner's profile if arg.Version is the stored
// version, and raises it by one. A stale or missing version is ErrVersionConflict.
func (r *Repo) UpdateProfile(ctx context.Context, arg db.UpdateProfileParams) (db.Profile, error) {
	row, err := r.q.UpdateProfile(ctx, arg)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Profile{}, ErrVersionConflict
	}
	return row, wrap("update profile", err)
}

// ReplaceProfileRoles makes roles the owner's whole list of roles, in order.
func (r *Repo) ReplaceProfileRoles(ctx context.Context, owner uuid.UUID, roles []db.InsertProfileRoleParams) error {
	if err := r.q.DeleteProfileRoles(ctx, owner); err != nil {
		return wrap("delete profile roles", err)
	}
	for _, role := range roles {
		role.OwnerID = owner
		if err := r.q.InsertProfileRole(ctx, role); err != nil {
			return wrap("insert profile role", err)
		}
	}
	return nil
}

// ListProfileRoles returns the owner's roles in the order they were saved.
func (r *Repo) ListProfileRoles(ctx context.Context, owner uuid.UUID) ([]db.ProfileRole, error) {
	rows, err := r.q.ListProfileRoles(ctx, owner)
	return rows, wrap("list profile roles", err)
}

// UpsertSkill adds a skill, or raises the level of one the owner has. It never
// lowers a level, and an existing skill keeps its name and source.
func (r *Repo) UpsertSkill(ctx context.Context, arg db.UpsertSkillParams) error {
	return wrap("upsert skill", r.q.UpsertSkill(ctx, arg))
}

// ListSkills returns the owner's skills, strongest first.
func (r *Repo) ListSkills(ctx context.Context, owner uuid.UUID) ([]db.Skill, error) {
	rows, err := r.q.ListSkills(ctx, owner)
	return rows, wrap("list skills", err)
}

// MonthPtr returns nil for an empty month, else a pointer to it.
func MonthPtr(s string) *string { return textPtr(s) }
