package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/owners"
	"github.com/0xHoaxen/shogun/pkg/owners/ownerstest"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
	"github.com/0xHoaxen/shogun/services/kagami/migrations"
)

func purgePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "kagami")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

// seedOwner gives the owner a row in every table, linked the way the app links them.
func seedOwner(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID) {
	t.Helper()
	company, job, contact := uuid.New(), uuid.New(), uuid.New()
	for _, seed := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO companies (id, owner_id, name) VALUES ($1, $2, 'Northwind')`, []any{company, owner}},
		{`INSERT INTO jobs (id, owner_id, company_id, title) VALUES ($1, $2, $3, 'Engineer')`, []any{job, owner, company}},
		{`INSERT INTO job_events (id, job_id, kind) VALUES (gen_random_uuid(), $1, 'created')`, []any{job}},
		{`INSERT INTO contacts (id, owner_id, full_name, company_id, job_id) VALUES ($1, $2, 'Ada', $3, $4)`, []any{contact, owner, company, job}},
		{`INSERT INTO contact_events (id, contact_id, kind) VALUES (gen_random_uuid(), $1, 'created')`, []any{contact}},
		{`INSERT INTO imports (id, owner_id, filename, rows_total, rows_created, rows_updated, rows_failed) VALUES (gen_random_uuid(), $1, 'a.csv', 1, 1, 0, 0)`, []any{owner}},
	} {
		if _, err := pool.Exec(context.Background(), seed.sql, seed.args...); err != nil {
			t.Fatalf("seed %q: %v", seed.sql, err)
		}
	}
}

func rowCount(t *testing.T, pool *pgxpool.Pool, table string, owner uuid.UUID) int {
	t.Helper()
	// Event tables have no owner_id, so they are counted through their parent.
	q := map[string]string{
		"job_events":     `SELECT count(*) FROM job_events e JOIN jobs j ON j.id = e.job_id WHERE j.owner_id = $1`,
		"contact_events": `SELECT count(*) FROM contact_events e JOIN contacts c ON c.id = e.contact_id WHERE c.owner_id = $1`,
	}[table]
	if q == "" {
		q = `SELECT count(*) FROM ` + table + ` WHERE owner_id = $1`
	}
	var n int
	if err := pool.QueryRow(context.Background(), q, owner).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

var purgedTables = []string{"companies", "jobs", "job_events", "contacts", "contact_events", "imports"}

func TestPurgeRemovesEverythingTheUserOwnsAndNothingElse(t *testing.T) {
	// Arrange
	ctx := context.Background()
	pool := purgePool(t)
	gone, kept := uuid.New(), uuid.New()
	seedOwner(t, pool, gone)
	seedOwner(t, pool, kept)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	// Act
	err = owners.PurgeSQL(store.PurgeStatements...)(ctx, tx, gone)
	if err == nil {
		err = tx.Commit(ctx)
	}

	// Assert
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	for _, table := range purgedTables {
		if n := rowCount(t, pool, table, gone); n != 0 {
			t.Errorf("%s: %d rows of the deleted user remain", table, n)
		}
		if n := rowCount(t, pool, table, kept); n != 1 {
			t.Errorf("%s: %d rows of the other user, want 1", table, n)
		}
	}
}

func TestPurgeForAUserWithNoDataIsHarmless(t *testing.T) {
	ctx := context.Background()
	pool := purgePool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	err = owners.PurgeSQL(store.PurgeStatements...)(ctx, tx, uuid.New())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
}

func TestPurgeCoversEveryTableOfTheSchema(t *testing.T) {
	ownerstest.RequireCovered(t, purgePool(t), store.PurgeStatements)
}
