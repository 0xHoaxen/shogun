package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/owners/ownerstest"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
	"github.com/0xHoaxen/shogun/services/taiko/migrations"
)

func purgePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "taiko")
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
			`INSERT INTO notifications (id, owner_id, type, title) VALUES (gen_random_uuid(), $1, 'daily_digest', 'Your day')`,
			`INSERT INTO channel_settings (owner_id, channel) VALUES ($1, 'in_app')`,
		} {
			if _, err := pool.Exec(context.Background(), stmt, owner); err != nil {
				t.Fatalf("seed %q: %v", stmt, err)
			}
		}
	}

	ownerstest.RequirePurgeIsolated(t, pool, store.PurgeStatements, seed, map[string]string{
		"notifications":    `SELECT count(*) FROM notifications WHERE owner_id = $1`,
		"channel_settings": `SELECT count(*) FROM channel_settings WHERE owner_id = $1`,
	})
}

func TestPurgeCoversEveryTableOfTheSchema(t *testing.T) {
	ownerstest.RequireCovered(t, purgePool(t), store.PurgeStatements)
}
