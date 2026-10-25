package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/owners/ownerstest"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
	"github.com/0xHoaxen/shogun/services/katana/migrations"
)

func purgePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "katana")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func TestPurgeRemovesEverythingTheUserOwnsIncludingTheTokenAndNothingElse(t *testing.T) {
	pool := purgePool(t)
	seed := func(t *testing.T, owner uuid.UUID) {
		t.Helper()
		for _, stmt := range []string{
			`INSERT INTO github_accounts (owner_id, login, token_ciphertext, key_id) VALUES ($1, 'octocat', '\x01', 'k')`,
			`INSERT INTO github_snapshots (id, owner_id, repos, contributions) VALUES (gen_random_uuid(), $1, '[]', '{}')`,
			`INSERT INTO suggestions (id, owner_id, target, section, after, reason) VALUES (gen_random_uuid(), $1, 'resume', 'skills', 'Go', 'because')`,
			`INSERT INTO profiles (owner_id, name) VALUES ($1, 'Priya')`,
			`INSERT INTO profile_roles (owner_id, position, title) VALUES ($1, 0, 'Engineer')`,
			`INSERT INTO skills (owner_id, name_key, name, level, source) VALUES ($1, 'go', 'Go', 5, 'resume')`,
			`INSERT INTO job_skill_runs (owner_id, job_id) VALUES ($1, gen_random_uuid())`,
			`INSERT INTO job_skills (owner_id, job_id, name_key, name) VALUES ($1, gen_random_uuid(), 'go', 'Go')`,
		} {
			if _, err := pool.Exec(context.Background(), stmt, owner); err != nil {
				t.Fatalf("seed %q: %v", stmt, err)
			}
		}
	}

	ownerstest.RequirePurgeIsolated(t, pool, store.PurgeStatements, seed, map[string]string{
		"github_accounts":  `SELECT count(*) FROM github_accounts WHERE owner_id = $1`,
		"github_snapshots": `SELECT count(*) FROM github_snapshots WHERE owner_id = $1`,
		"suggestions":      `SELECT count(*) FROM suggestions WHERE owner_id = $1`,
		"profiles":         `SELECT count(*) FROM profiles WHERE owner_id = $1`,
		"profile_roles":    `SELECT count(*) FROM profile_roles WHERE owner_id = $1`,
		"skills":           `SELECT count(*) FROM skills WHERE owner_id = $1`,
		"job_skill_runs":   `SELECT count(*) FROM job_skill_runs WHERE owner_id = $1`,
		"job_skills":       `SELECT count(*) FROM job_skills WHERE owner_id = $1`,
	})
}

func TestPurgeCoversEveryTableOfTheSchema(t *testing.T) {
	ownerstest.RequireCovered(t, purgePool(t), store.PurgeStatements)
}
