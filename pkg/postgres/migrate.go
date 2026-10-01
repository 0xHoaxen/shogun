package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
)

// Migrate applies pending goose migrations found at the root of fsys. The
// schema named in the pool's search_path is created if missing, and goose's
// version table lives inside it.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (err error) {
	schema := pool.Config().ConnConfig.RuntimeParams["search_path"]
	if err := ValidateSchema(schema); err != nil {
		return fmt.Errorf("postgres: pool not created by Connect: %w", err)
	}
	if err := ensureSchema(ctx, pool, schema); err != nil {
		return err
	}

	db := stdlib.OpenDBFromPool(pool)
	defer func() {
		// Closing the sql.DB does not close the shared pool.
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("postgres: close migration db: %w", closeErr))
		}
	}()

	provider, err := goose.NewProvider(database.DialectPostgres, db, fsys)
	if err != nil {
		return fmt.Errorf("postgres: create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("postgres: apply migrations: %w", err)
	}
	return nil
}

// ensureSchema creates schema only when absent, so a service role that owns
// the schema but cannot create schemas still works.
func ensureSchema(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, schema).Scan(&exists)
	if err != nil {
		return fmt.Errorf("postgres: check schema: %w", err)
	}
	if exists {
		return nil
	}
	// schema is validated as a plain identifier, so quoting is sufficient.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %q`, schema)); err != nil {
		return fmt.Errorf("postgres: create schema: %w", err)
	}
	return nil
}
