package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
)

// GitHubAccount returns the owner's GitHub connection, or ErrNotFound.
func (r *Repo) GitHubAccount(ctx context.Context, owner uuid.UUID) (db.GithubAccount, error) {
	row, err := r.q.GetGitHubAccount(ctx, owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GithubAccount{}, ErrNotFound
	}
	return row, wrap("get github account", err)
}

// SaveGitHubAccount stores the owner's connection, replacing any earlier one.
func (r *Repo) SaveGitHubAccount(ctx context.Context, arg db.UpsertGitHubAccountParams) (db.GithubAccount, error) {
	row, err := r.q.UpsertGitHubAccount(ctx, arg)
	return row, wrap("save github account", err)
}

// DeleteGitHubAccount removes the owner's connection and token. It reports
// whether there was one.
func (r *Repo) DeleteGitHubAccount(ctx context.Context, owner uuid.UUID) (bool, error) {
	n, err := r.q.DeleteGitHubAccount(ctx, owner)
	return n > 0, wrap("delete github account", err)
}

// GitHubAccountOwners returns every owner with a GitHub connection.
func (r *Repo) GitHubAccountOwners(ctx context.Context) ([]uuid.UUID, error) {
	ids, err := r.q.ListGitHubAccountOwners(ctx)
	return ids, wrap("list github account owners", err)
}
