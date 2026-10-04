package store_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/store"
	"github.com/0xHoaxen/shogun/services/torii/migrations"
)

var storeNow = time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func newSessions(t *testing.T) (*store.Sessions, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "torii")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.NewSessions(pool), pool
}

func newSession(expiresAt time.Time) domain.Session {
	return domain.Session{
		ID:          store.NewID(),
		OwnerID:     store.NewID(),
		Email:       "owner@example.com",
		DisplayName: "Owner",
		ExpiresAt:   expiresAt,
	}
}

func TestInsertThenLookupByTokenHash(t *testing.T) {
	// Arrange
	sessions, _ := newSessions(t)
	want := newSession(storeNow.Add(time.Hour))
	hash := []byte("hash-1")

	// Act
	insertErr := sessions.Insert(context.Background(), want, hash)
	got, lookupErr := sessions.ByTokenHash(context.Background(), hash)

	// Assert
	if insertErr != nil || lookupErr != nil {
		t.Fatalf("insert %v, lookup %v", insertErr, lookupErr)
	}
	if got.ID != want.ID || got.OwnerID != want.OwnerID || got.Email != want.Email ||
		got.DisplayName != want.DisplayName || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLookupUnknownHashIsNotFound(t *testing.T) {
	// Arrange
	sessions, _ := newSessions(t)

	// Act
	_, err := sessions.ByTokenHash(context.Background(), []byte("missing"))

	// Assert
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestInsertRejectsDuplicateTokenHash(t *testing.T) {
	// Arrange
	sessions, _ := newSessions(t)
	hash := []byte("same-hash")
	if err := sessions.Insert(context.Background(), newSession(storeNow), hash); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// Act
	err := sessions.Insert(context.Background(), newSession(storeNow), hash)

	// Assert
	if err == nil {
		t.Fatal("second insert with the same token hash succeeded")
	}
}

func TestExtendMovesExpiry(t *testing.T) {
	// Arrange
	sessions, _ := newSessions(t)
	session := newSession(storeNow.Add(time.Hour))
	hash := []byte("hash-extend")
	if err := sessions.Insert(context.Background(), session, hash); err != nil {
		t.Fatalf("insert: %v", err)
	}
	later := storeNow.Add(48 * time.Hour)

	// Act
	extendErr := sessions.Extend(context.Background(), session.ID, later)
	got, lookupErr := sessions.ByTokenHash(context.Background(), hash)
	missingErr := sessions.Extend(context.Background(), store.NewID(), later)

	// Assert
	if extendErr != nil || lookupErr != nil {
		t.Fatalf("extend %v, lookup %v", extendErr, lookupErr)
	}
	if !got.ExpiresAt.Equal(later) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, later)
	}
	if !errors.Is(missingErr, store.ErrNotFound) {
		t.Fatalf("extend of missing session = %v, want ErrNotFound", missingErr)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	// Arrange
	sessions, _ := newSessions(t)
	hash := []byte("hash-delete")
	if err := sessions.Insert(context.Background(), newSession(storeNow), hash); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Act
	first := sessions.Delete(context.Background(), hash)
	second := sessions.Delete(context.Background(), hash)
	_, lookupErr := sessions.ByTokenHash(context.Background(), hash)

	// Assert
	if first != nil || second != nil {
		t.Fatalf("delete errors: %v, %v", first, second)
	}
	if !errors.Is(lookupErr, store.ErrNotFound) {
		t.Fatalf("lookup after delete = %v, want ErrNotFound", lookupErr)
	}
}

func TestDeleteExpiredKeepsLiveSessions(t *testing.T) {
	// Arrange
	sessions, _ := newSessions(t)
	expired := newSession(storeNow.Add(-time.Minute))
	live := newSession(storeNow.Add(time.Hour))
	if err := sessions.Insert(context.Background(), expired, []byte("expired")); err != nil {
		t.Fatalf("insert expired: %v", err)
	}
	if err := sessions.Insert(context.Background(), live, []byte("live")); err != nil {
		t.Fatalf("insert live: %v", err)
	}

	// Act
	removed, err := sessions.DeleteExpired(context.Background(), storeNow)
	_, expiredErr := sessions.ByTokenHash(context.Background(), []byte("expired"))
	_, liveErr := sessions.ByTokenHash(context.Background(), []byte("live"))

	// Assert
	if err != nil || removed != 1 {
		t.Fatalf("removed %d, err %v, want 1", removed, err)
	}
	if !errors.Is(expiredErr, store.ErrNotFound) || liveErr != nil {
		t.Fatalf("expired lookup %v, live lookup %v", expiredErr, liveErr)
	}
}
