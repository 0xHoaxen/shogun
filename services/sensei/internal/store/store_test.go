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
