package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/owners/ownerstest"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
	"github.com/0xHoaxen/shogun/services/shinobi/migrations"
)

func purgePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "shinobi")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func TestPurgeRemovesEverythingTheUserOwnsAndNothingElse(t *testing.T) {
	pool := purgePool(t)
	seed := func(t *testing.T, owner uuid.UUID) {
		t.Helper()
		source, posting := uuid.New(), uuid.New()
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO sources (id, owner_id, name, kind, config) VALUES ($1, $2, 'Feed', 'rss', '{}')`, []any{source, owner}},
			{`INSERT INTO postings (id, owner_id, source_id, external_id, title, raw) VALUES ($1, $2, $3, 'x1', 'Engineer', '{}')`, []any{posting, owner, source}},
			{`INSERT INTO scores (posting_id, score, reasons, scored_by) VALUES ($1, 0.9, '[]', 'rule')`, []any{posting}},
			{`INSERT INTO preferences (owner_id) VALUES ($1)`, []any{owner}},
		} {
			if _, err := pool.Exec(context.Background(), s.sql, s.args...); err != nil {
				t.Fatalf("seed %q: %v", s.sql, err)
			}
		}
	}

	ownerstest.RequirePurgeIsolated(t, pool, store.PurgeStatements, seed, map[string]string{
		"sources":     `SELECT count(*) FROM sources WHERE owner_id = $1`,
		"postings":    `SELECT count(*) FROM postings WHERE owner_id = $1`,
		"scores":      `SELECT count(*) FROM scores s JOIN postings p ON p.id = s.posting_id WHERE p.owner_id = $1`,
		"preferences": `SELECT count(*) FROM preferences WHERE owner_id = $1`,
	})
}

func TestPurgeCoversEveryTableOfTheSchema(t *testing.T) {
	ownerstest.RequireCovered(t, purgePool(t), store.PurgeStatements)
}
