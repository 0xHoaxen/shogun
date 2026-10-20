package owners_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/owners"
)

func TestPurgeSQLRunsEachStatementInOrderForOneOwnerOnly(t *testing.T) {
	// Arrange
	ctx, pool := newPool(t)
	mine, theirs := uuid.New(), uuid.New()
	for _, o := range []uuid.UUID{mine, theirs} {
		if err := owners.Upsert(ctx, pool, owners.Owner{ID: o, Timezone: "UTC", Status: owners.StatusActive, Version: 1}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	// Act
	err = owners.PurgeSQL(`UPDATE owners SET version = version + 1 WHERE id = $1`, `UPDATE owners SET version = version + 10 WHERE id = $1`)(ctx, tx, mine)
	if err == nil {
		err = tx.Commit(ctx)
	}

	// Assert
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	got, _ := owners.Get(ctx, pool, mine)
	other, _ := owners.Get(ctx, pool, theirs)
	if got.Version != 12 || other.Version != 1 {
		t.Fatalf("mine version %d, theirs %d; want 12 and 1", got.Version, other.Version)
	}
}

func TestPurgeSQLReportsTheStatementThatFailed(t *testing.T) {
	ctx, pool := newPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	err = owners.PurgeSQL(`SELECT * FROM no_such_table WHERE id = $1`)(ctx, tx, uuid.New())

	if err == nil {
		t.Fatal("a failing statement was not reported")
	}
}
