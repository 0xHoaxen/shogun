package postgres_test

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
)

//go:embed testdata/migrations/*.sql
var migrationsFS embed.FS

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func migrations(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(migrationsFS, "testdata/migrations")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}
	return sub
}

func newMigratedPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "kagami")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations(t)); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return ctx, pool
}

func TestConnectRejectsInvalidSchema(t *testing.T) {
	for _, schema := range []string{"", "Bad", "a-b", "x; select 1", "1abc"} {
		if _, err := postgres.Connect(context.Background(), "postgres://localhost/x", schema); err == nil {
			t.Errorf("schema %q: expected error", schema)
		}
	}
}

func TestConnectFailsWhenServerUnreachable(t *testing.T) {
	_, err := postgres.Connect(context.Background(), "postgres://u:p@127.0.0.1:1/x?connect_timeout=1", "kagami")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMigrateCreatesTablesInServiceSchema(t *testing.T) {
	ctx, pool := newMigratedPool(t)

	// Idempotent.
	if err := postgres.Migrate(ctx, pool, migrations(t)); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	for _, table := range []string{"items", "goose_db_version"} {
		var schema string
		err := pool.QueryRow(ctx,
			`SELECT table_schema FROM information_schema.tables WHERE table_name = $1`, table).Scan(&schema)
		if err != nil || schema != "kagami" {
			t.Fatalf("%s schema = %q, err = %v; want kagami", table, schema, err)
		}
	}
}

func TestInTxCommitsOnSuccess(t *testing.T) {
	ctx, pool := newMigratedPool(t)

	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO items (name) VALUES ('a')`)
		return err
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if got := countItems(ctx, t, pool); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
}

func TestInTxRollsBackOnError(t *testing.T) {
	ctx, pool := newMigratedPool(t)
	boom := errors.New("boom")

	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO items (name) VALUES ('a')`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapping boom", err)
	}
	if got := countItems(ctx, t, pool); got != 0 {
		t.Fatalf("rows = %d, want 0 after rollback", got)
	}
}

func TestInTxRollsBackOnPanic(t *testing.T) {
	ctx, pool := newMigratedPool(t)

	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected panic to propagate")
			}
		}()
		_ = postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO items (name) VALUES ('a')`); err != nil {
				return err
			}
			panic("kaboom")
		})
	}()

	if got := countItems(ctx, t, pool); got != 0 {
		t.Fatalf("rows = %d, want 0 after panic", got)
	}
}

func countItems(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM items`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
