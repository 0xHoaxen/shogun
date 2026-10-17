// Package ownerstest helps a service prove its purge covers every table.
package ownerstest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// bookkeeping are the tables every service has that hold no user data.
var bookkeeping = []string{"owners", "outbox", "inbox", "goose_db_version"}

// RequireCovered fails the test when a table of the migrated schema is named by
// none of the purge statements, so a table added later cannot be forgotten.
// except lists tables that hold no per-user data, such as a shared price list.
func RequireCovered(t *testing.T, pool *pgxpool.Pool, statements []string, except ...string) {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE' ORDER BY 1`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if slices.Contains(bookkeeping, name) || slices.Contains(except, name) || strings.HasPrefix(name, "river") {
			continue
		}
		if !mentioned(statements, name) {
			t.Errorf("table %q is in the schema but no purge statement touches it, so a deleted user's rows would stay", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read tables: %v", err)
	}
}

// mentioned reports whether a statement uses name as a whole word.
func mentioned(statements []string, name string) bool {
	for _, stmt := range statements {
		for _, word := range strings.FieldsFunc(stmt, func(r rune) bool {
			return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
		}) {
			if word == name {
				return true
			}
		}
	}
	return false
}
