package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store"
	"github.com/0xHoaxen/shogun/services/sensei/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func TestInsertFactStoresOncePerEventAndCountsPerOwnerAndType(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "sensei")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := store.New(pool)
	owner := uuid.New()
	fact := domain.Fact{EventID: uuid.New(), OwnerID: owner, Type: "job.added", OccurredAt: time.Now()}

	first, firstErr := repo.InsertFact(ctx, fact)
	again, againErr := repo.InsertFact(ctx, fact)
	_, _ = repo.InsertFact(ctx, domain.Fact{EventID: uuid.New(), OwnerID: owner, Type: "job.added", OccurredAt: time.Now()})
	_, _ = repo.InsertFact(ctx, domain.Fact{EventID: uuid.New(), OwnerID: owner, Type: "mail.reply_detected", OccurredAt: time.Now()})
	_, _ = repo.InsertFact(ctx, domain.Fact{EventID: uuid.New(), OwnerID: uuid.New(), Type: "job.added", OccurredAt: time.Now()})
	n, countErr := repo.CountFacts(ctx, owner, "job.added")

	if !first || firstErr != nil || again || againErr != nil || n != 2 || countErr != nil {
		t.Fatalf("first %v %v, again %v %v, count %d %v; want a repeat ignored and two job.added for the owner", first, firstErr, again, againErr, n, countErr)
	}
}

func TestListRollupsReturnsOnlyTheOwnersNumbers(t *testing.T) {
	// Arrange
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "sensei")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := store.New(pool)
	owner, other := uuid.New(), uuid.New()
	when := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	for _, id := range []uuid.UUID{owner, other, other} {
		fact := domain.Fact{EventID: uuid.New(), OwnerID: id, Type: "job.added", OccurredAt: when, Dimension: map[string]string{"source": "manual"}}
		if _, err := repo.InsertFact(ctx, fact); err != nil {
			t.Fatalf("InsertFact: %v", err)
		}
	}
	day := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

	// Act
	if err := repo.RebuildRollups(ctx, day); err != nil {
		t.Fatalf("RebuildRollups: %v", err)
	}
	mine, mineErr := repo.ListRollups(ctx, owner, day, day, []string{"jobs_added"})
	theirs, theirsErr := repo.ListRollups(ctx, other, day, day, []string{"jobs_added"})

	// Assert
	if mineErr != nil || theirsErr != nil {
		t.Fatalf("ListRollups: %v, %v", mineErr, theirsErr)
	}
	if len(mine) != 1 || mine[0].Value != 1 || len(theirs) != 1 || theirs[0].Value != 2 {
		t.Fatalf("owner got %+v and the other owner %+v, want one job each owner counted separately (1 and 2)", mine, theirs)
	}
}
