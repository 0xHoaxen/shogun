// Package postgres provides the pgx connection pool, goose migrations and
// transaction helper shared by every service. Each service owns one schema;
// Connect pins search_path to it, followed by the shared extensions schema, so
// SQL never needs qualified names.
package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// maxIdentifierLen is the longest identifier Postgres accepts.
const maxIdentifierLen = 63

// extensionsSchema holds the shared vector and citext extensions. It follows
// the service schema on search_path so their operators resolve unqualified.
const extensionsSchema = "extensions"

var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// ValidateSchema reports whether schema is a safe, lowercase Postgres identifier.
func ValidateSchema(schema string) error {
	if len(schema) > maxIdentifierLen || !identifierPattern.MatchString(schema) {
		return fmt.Errorf("postgres: invalid schema name %q", schema)
	}
	return nil
}

// Connect opens a pool whose connections have search_path pinned to schema
// followed by the shared extensions schema, and pings the server before
// returning.
func Connect(ctx context.Context, url, schema string) (*pgxpool.Pool, error) {
	if err := ValidateSchema(schema); err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + "," + extensionsSchema

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

// SchemaOf returns the service schema a pool created by Connect is pinned to,
// the first entry of its search_path.
func SchemaOf(pool *pgxpool.Pool) (string, error) {
	schema, _, _ := strings.Cut(pool.Config().ConnConfig.RuntimeParams["search_path"], ",")
	if err := ValidateSchema(schema); err != nil {
		return "", fmt.Errorf("postgres: pool not created by Connect: %w", err)
	}
	return schema, nil
}
