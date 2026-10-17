package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/owners"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/kagami/migrations"
)

func TestMigrationsGiveTheOwnersTableTheSharedPackageUses(t *testing.T) {
	// Arrange
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "kagami")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	want := owners.Owner{ID: uuid.New(), Timezone: "Europe/Berlin", Status: owners.StatusActive, Version: 1}

	// Act
	upsertErr := owners.Upsert(ctx, pool, want)
	got, listErr := owners.ListActive(ctx, pool)

	// Assert
	if upsertErr != nil || listErr != nil {
		t.Fatalf("upsert %v, list %v", upsertErr, listErr)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ListActive = %+v, want [%+v]", got, want)
	}
}
