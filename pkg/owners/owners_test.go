package owners_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/owners"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func newPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "kagami")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, owners.SQL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return ctx, pool
}

func owner(version int32, status, tz string) owners.Owner {
	return owners.Owner{ID: uuid.New(), Timezone: tz, Status: status, Version: version}
}

func TestUpsertStoresAnOwnerAndListActiveReturnsIt(t *testing.T) {
	// Arrange
	ctx, pool := newPool(t)
	want := owner(1, owners.StatusActive, "Europe/Berlin")

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

func TestUpsertAppliesOnlyNewerVersions(t *testing.T) {
	// Arrange
	ctx, pool := newPool(t)
	first := owner(2, owners.StatusActive, "Europe/Berlin")
	if err := owners.Upsert(ctx, pool, first); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Act
	older := first
	older.Version, older.Timezone = 1, "Asia/Tokyo"
	same := first
	same.Timezone = "Asia/Tokyo"
	newer := first
	newer.Version, newer.Timezone = 3, "America/New_York"
	errs := []error{owners.Upsert(ctx, pool, older), owners.Upsert(ctx, pool, same)}
	afterStale, _ := owners.Get(ctx, pool, first.ID)
	errs = append(errs, owners.Upsert(ctx, pool, newer))
	afterNewer, _ := owners.Get(ctx, pool, first.ID)

	// Assert
	for _, err := range errs {
		if err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	if afterStale != first {
		t.Fatalf("a stale or repeated event changed the owner to %+v", afterStale)
	}
	if afterNewer != newer {
		t.Fatalf("owner = %+v, want the newer %+v", afterNewer, newer)
	}
}

func TestListActiveSkipsDisabledAndDeletedOwners(t *testing.T) {
	// Arrange
	ctx, pool := newPool(t)
	active := owner(1, owners.StatusActive, "UTC")
	for _, o := range []owners.Owner{active, owner(1, owners.StatusDisabled, "UTC"), owner(1, owners.StatusDeleted, "UTC")} {
		if err := owners.Upsert(ctx, pool, o); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	// Act
	got, err := owners.ListActive(ctx, pool)

	// Assert
	if err != nil || len(got) != 1 || got[0].ID != active.ID {
		t.Fatalf("ListActive = %+v, %v, want only the active owner", got, err)
	}
}

func TestGetUnknownOwnerIsNotFound(t *testing.T) {
	ctx, pool := newPool(t)

	_, err := owners.Get(ctx, pool, uuid.New())

	if !errors.Is(err, owners.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpsertRejectsAnInvalidOwner(t *testing.T) {
	ctx, pool := newPool(t)
	tests := map[string]owners.Owner{
		"no id":          {Timezone: "UTC", Status: owners.StatusActive, Version: 1},
		"unknown status": {ID: uuid.New(), Timezone: "UTC", Status: "weird", Version: 1},
		"no timezone":    {ID: uuid.New(), Status: owners.StatusActive, Version: 1},
	}
	for name, o := range tests {
		t.Run(name, func(t *testing.T) {
			if err := owners.Upsert(ctx, pool, o); err == nil {
				t.Fatal("Upsert succeeded, want an error")
			}
		})
	}
}

func TestLocationFallsBackForAnUnknownZone(t *testing.T) {
	good := owners.Owner{Timezone: "Europe/Berlin"}.Location()
	bad := owners.Owner{Timezone: "Not/AZone"}.Location()
	def := owners.Owner{Timezone: owners.DefaultTimezone}.Location()

	if good.String() != "Europe/Berlin" || bad.String() != def.String() {
		t.Fatalf("zones %q and %q, want Europe/Berlin and the default %q", good, bad, def)
	}
}

func TestLocalDateAndTimeFollowTheOwnersZone(t *testing.T) {
	// 2026-10-07 22:00 UTC is already the 8th in Tokyo and still the 7th in New York.
	now := time.Date(2026, 10, 7, 22, 0, 0, 0, time.UTC)
	tokyo := owners.Owner{Timezone: "Asia/Tokyo"}
	newYork := owners.Owner{Timezone: "America/New_York"}

	if tokyo.LocalDate(now) != "2026-10-08" || newYork.LocalDate(now) != "2026-10-07" {
		t.Fatalf("dates %s and %s, want 2026-10-08 and 2026-10-07", tokyo.LocalDate(now), newYork.LocalDate(now))
	}
	// It is 07:00 in Tokyo and 18:00 in New York.
	if tokyo.ReachedLocalTime(now, 8, 0) || !tokyo.ReachedLocalTime(now, 7, 0) || !newYork.ReachedLocalTime(now, 8, 0) {
		t.Fatal("ReachedLocalTime does not follow each owner's clock")
	}
}
