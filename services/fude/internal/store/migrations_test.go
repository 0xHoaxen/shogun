package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/fude/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func TestMigrationsCreateDraftTables(t *testing.T) {
	// Arrange
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "fude")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// Act
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Assert
	tests := []struct {
		name  string
		query string
	}{
		{"drafts, citext recipient and defaults", `INSERT INTO drafts (id, owner_id, kind, target_type, channel, recipient)
			VALUES (gen_random_uuid(), gen_random_uuid(), 'cover_letter', 'job', 'email', 'A@Example.com')`},
		{"voice sample with a 1024-dim embedding", `INSERT INTO voice_samples (id, owner_id, channel, text, embedding)
			VALUES (gen_random_uuid(), gen_random_uuid(), 'email', 'hi', array_fill(0.1::real, ARRAY[1024])::vector)`},
		{"template with a null contact status", `INSERT INTO templates (id, owner_id, kind, channel, instructions)
			VALUES (gen_random_uuid(), gen_random_uuid(), 'cover_letter', 'email', 'x')`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, tt.query); err != nil {
				t.Fatalf("exec: %v", err)
			}
		})
	}

	t.Run("recipient compares case-insensitively", func(t *testing.T) {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM drafts WHERE recipient = 'a@example.com'`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("count %d, err %v", n, err)
		}
	})
	t.Run("draft state CHECK rejects unknown values", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO drafts (id, owner_id, kind, target_type, channel, state)
			VALUES (gen_random_uuid(), gen_random_uuid(), 'post', 'none', 'x', 'bogus')`)
		if err == nil {
			t.Fatal("want CHECK violation")
		}
	})
	t.Run("duplicate template for the same null status is rejected", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO templates (id, owner_id, kind, channel, instructions)
			SELECT gen_random_uuid(), owner_id, kind, channel, 'y' FROM templates LIMIT 1`)
		if err == nil {
			t.Fatal("want unique violation")
		}
	})
}
