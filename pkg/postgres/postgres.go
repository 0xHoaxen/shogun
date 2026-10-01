// Package postgres provides the pgx connection pool, goose migrations and
// transaction helper shared by every service. Each service owns one schema;
// Connect pins search_path to it so SQL never needs qualified names.
package postgres

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"
)

// maxIdentifierLen is the longest identifier Postgres accepts.
const maxIdentifierLen = 63

var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// ValidateSchema reports whether schema is a safe, lowercase Postgres identifier.
func ValidateSchema(schema string) error {
	if len(schema) > maxIdentifierLen || !identifierPattern.MatchString(schema) {
		return fmt.Errorf("postgres: invalid schema name %q", schema)
	}
	return nil
}

// Connect opens a pool whose connections have search_path pinned to schema,
// and pings the server before returning.
func Connect(ctx context.Context, url, schema string) (*pgxpool.Pool, error) {
	if err := ValidateSchema(schema); err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, nil
}
