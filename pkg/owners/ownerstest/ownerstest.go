// Package ownerstest helps a service prove its purge covers every table.
package ownerstest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/owners"
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
			return r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
		}) {
			if word == name {
				return true
			}
		}
	}
	return false
}

// RequirePurgeIsolated seeds two owners with seed, purges one with statements
// and checks the other is untouched. counts maps each table to a query that
// takes the owner id as $1 and returns how many rows that owner has there.
// Every table must hold rows for both owners before the purge, so a seed that
// forgets a table fails instead of passing vacuously.
func RequirePurgeIsolated(t *testing.T, pool *pgxpool.Pool, statements []string, seed func(t *testing.T, owner uuid.UUID), counts map[string]string) {
	t.Helper()
	ctx := context.Background()
	gone, kept := uuid.New(), uuid.New()
	seed(t, gone)
	seed(t, kept)
	count := func(table string, owner uuid.UUID) int {
		var n int
		if err := pool.QueryRow(ctx, counts[table], owner).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	before := map[string]int{}
	for table := range counts {
		before[table] = count(table, kept)
		if before[table] == 0 || count(table, gone) == 0 {
			t.Fatalf("the seed left %s empty for an owner, so the purge would prove nothing", table)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	err = owners.PurgeSQL(statements...)(ctx, tx, gone)
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	for table := range counts {
		if n := count(table, gone); n != 0 {
			t.Errorf("%s: %d rows of the deleted user remain", table, n)
		}
		if n := count(table, kept); n != before[table] {
			t.Errorf("%s: the other user has %d rows, had %d", table, n, before[table])
		}
	}
}
