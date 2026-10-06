package app_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/domain"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
	"github.com/0xHoaxen/shogun/services/taiko/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

var now = time.Date(2026, 10, 7, 8, 30, 0, 0, time.UTC)

func newService(t *testing.T) (*app.Service, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "taiko")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return app.NewService(pool, func() time.Time { return now }), pool
}

func record(t *testing.T, svc *app.Service, pool *pgxpool.Pool, in app.RecordInput) error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := svc.Record(ctx, tx, in); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func input(t *testing.T, owner, event uuid.UUID) app.RecordInput {
	t.Helper()
	n, err := domain.DraftReady(uuid.NewString(), "cover_letter")
	if err != nil {
		t.Fatalf("notice: %v", err)
	}
	return app.RecordInput{OwnerID: owner, EventID: event, Notice: n}
}

func TestRecordStoresTheNotificationAtTheClockTime(t *testing.T) {
	svc, pool := newService(t)
	owner := uuid.New()

	err := record(t, svc, pool, input(t, owner, uuid.New()))

	rows, _, listErr := store.New(pool).List(context.Background(), owner, false, store.Page{})
	if err != nil || listErr != nil || len(rows) != 1 || !rows[0].CreatedAt.Equal(now) || rows[0].Title != "Cover letter ready" {
		t.Fatalf("err %v/%v, rows %+v", err, listErr, rows)
	}
}

func TestRecordTwiceForTheSameEventStoresOnce(t *testing.T) {
	svc, pool := newService(t)
	owner, event := uuid.New(), uuid.New()

	first := record(t, svc, pool, input(t, owner, event))
	again := record(t, svc, pool, input(t, owner, event))

	n, err := store.New(pool).CountUnread(context.Background(), owner)
	if first != nil || again != nil || err != nil || n != 1 {
		t.Fatalf("errs %v %v %v, unread %d; want 1", first, again, err, n)
	}
}

func TestRecordRollsBackWithItsTransaction(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()
	owner := uuid.New()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if err := svc.Record(ctx, tx, input(t, owner, uuid.New())); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if n, _ := store.New(pool).CountUnread(ctx, owner); n != 0 {
		t.Fatalf("unread %d after a rollback, want 0", n)
	}
}
