package jobs_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/pkg/bus/relay"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/jobs"
	"github.com/0xHoaxen/shogun/services/katana/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func TestRiverQueueInsertsOneRunPerSnapshotAndPerItemInTheCallersTransaction(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "katana")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := relay.Migrate(ctx, pool); err != nil {
		t.Fatalf("relay migrate: %v", err)
	}
	queue, err := jobs.NewRiverQueue(pool)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	owner, snapshot := uuid.New(), uuid.New()
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'suggest'`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	enqueue := func(in app.SuggestInput, commit bool) {
		err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
			if err := queue.EnqueueSuggest(ctx, tx, in); err != nil {
				return err
			}
			if !commit {
				return context.Canceled
			}
			return nil
		})
		if commit && err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	enqueue(app.SuggestInput{OwnerID: owner, SnapshotID: &snapshot}, false)
	afterRollback := count()
	enqueue(app.SuggestInput{OwnerID: owner, SnapshotID: &snapshot}, true)
	enqueue(app.SuggestInput{OwnerID: owner, SnapshotID: &snapshot}, true)
	afterSameSnapshot := count()
	item := &domain.LearnedItem{ItemID: "i1", Title: "Go course"}
	enqueue(app.SuggestInput{OwnerID: owner, Learned: item}, true)
	enqueue(app.SuggestInput{OwnerID: owner, Learned: item}, true)

	if afterRollback != 0 || afterSameSnapshot != 1 || count() != 2 {
		t.Fatalf("jobs: after rollback %d, after the same snapshot twice %d, after the same item twice %d; want 0, 1, 2",
			afterRollback, afterSameSnapshot, count())
	}
}
