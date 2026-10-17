package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/owners/ownerstest"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store"
	"github.com/0xHoaxen/shogun/services/sensei/migrations"
)

func purgePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "sensei")
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
		for _, stmt := range []string{
			`INSERT INTO facts (event_id, owner_id, type, occurred_at) VALUES (gen_random_uuid(), $1, 'job.added', now())`,
			`INSERT INTO daily_rollups (owner_id, day, metric, value) VALUES ($1, current_date, 'jobs_added', 3)`,
		} {
			if _, err := pool.Exec(context.Background(), stmt, owner); err != nil {
				t.Fatalf("seed %q: %v", stmt, err)
			}
		}
	}

	ownerstest.RequirePurgeIsolated(t, pool, store.PurgeStatements, seed, map[string]string{
		"facts":         `SELECT count(*) FROM facts WHERE owner_id = $1`,
		"daily_rollups": `SELECT count(*) FROM daily_rollups WHERE owner_id = $1`,
	})
}

func TestPurgeCoversEveryTableOfTheSchema(t *testing.T) {
	ownerstest.RequireCovered(t, purgePool(t), store.PurgeStatements)
}
