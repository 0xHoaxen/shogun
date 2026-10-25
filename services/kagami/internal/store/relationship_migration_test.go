package store_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/kagami/migrations"
)

// upTo returns the migrations whose file names sort at or before last.
func upTo(t *testing.T, last string) fs.FS {
	t.Helper()
	files, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	out := fstest.MapFS{}
	for _, f := range files {
		if f.Name() <= last {
			data, err := fs.ReadFile(migrations.FS, f.Name())
			if err != nil {
				t.Fatalf("read %s: %v", f.Name(), err)
			}
			out[f.Name()] = &fstest.MapFile{Data: data}
		}
	}
	return out
}

func TestRelationshipCheckTidiesOldRowsThenRefusesBadOnes(t *testing.T) {
	// Arrange: a database as it was before migration 00004, with the kinds of values
	// people typed or imported when nothing checked them.
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "kagami")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, upTo(t, "00003_owners.sql")); err != nil {
		t.Fatalf("migrate to 00003: %v", err)
	}
	owner := uuid.New()
	old := map[string]*string{
		"valid":          ptr("recruiter"),
		"shouting":       ptr("FRIEND"),
		"padded":         ptr("  Manager "),
		"unknown":        ptr("Referral"),
		"two words":      ptr("Hiring manager"),
		"blank":          ptr(""),
		"never answered": nil,
	}
	for name, rel := range old {
		if _, err := pool.Exec(ctx, `INSERT INTO contacts (id, owner_id, full_name, relationship) VALUES ($1, $2, $3, $4)`,
			uuid.New(), owner, name, rel); err != nil {
			t.Fatalf("insert %q: %v", name, err)
		}
	}

	// Act
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate to latest: %v", err)
	}

	// Assert
	want := map[string]*string{
		"valid": ptr("recruiter"), "shouting": ptr("friend"), "padded": ptr("manager"),
		"unknown": ptr("other"), "two words": ptr("other"), "blank": nil, "never answered": nil,
	}
	for name, wantRel := range want {
		var got *string
		if err := pool.QueryRow(ctx, `SELECT relationship FROM contacts WHERE full_name = $1`, name).Scan(&got); err != nil {
			t.Fatalf("read %q: %v", name, err)
		}
		if (got == nil) != (wantRel == nil) || (got != nil && *got != *wantRel) {
			t.Errorf("%s: got %v, want %v", name, deref(got), deref(wantRel))
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO contacts (id, owner_id, full_name, relationship) VALUES ($1, $2, 'x', 'colleague')`, uuid.New(), owner); err == nil {
		t.Error("the database accepted a relationship outside the six")
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
