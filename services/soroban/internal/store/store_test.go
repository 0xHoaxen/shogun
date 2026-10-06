package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
	"github.com/0xHoaxen/shogun/services/soroban/migrations"
)

func newRepo(t *testing.T) (context.Context, *store.Repo) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "soroban")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return ctx, store.New(pool)
}

func TestPriceOnReturnsThePriceInForceOnTheDate(t *testing.T) {
	ctx, repo := newRepo(t)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

	tests := []struct {
		name    string
		model   string
		date    time.Time
		wantIn  int64
		wantErr error
	}{
		{"on the effective date", "claude-opus-5-5", day(2026, 1, 1), 4_000_000, nil},
		{"after the effective date", "claude-opus-5-5", day(2026, 10, 5), 4_000_000, nil},
		{"before any price", "claude-opus-5-5", day(2025, 12, 31), 0, store.ErrNotFound},
		{"unknown model", "claude-unpriced", day(2026, 10, 5), 0, store.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.PriceOn(ctx, tt.model, tt.date)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got error %v, want %v", err, tt.wantErr)
			}
			if err == nil && got.InputMicrosPerMtok != tt.wantIn {
				t.Errorf("input rate = %d, want %d", got.InputMicrosPerMtok, tt.wantIn)
			}
		})
	}
}

func TestLockPeriodIsCreatedOnceAndReused(t *testing.T) {
	ctx, repo := newRepo(t)
	owner := store.NewID()
	if err := repo.EnsureDefaultBudgets(ctx, owner); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	budgets, err := repo.MatchingBudgets(ctx, owner, "fude", "fude.post")
	if err != nil || len(budgets) != 3 {
		t.Fatalf("matching budgets: %d, %v; want global, service and feature", len(budgets), err)
	}
	start := time.Date(2026, 9, 30, 18, 30, 0, 0, time.UTC)
	end := time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC)

	first, err := repo.LockPeriod(ctx, budgets[0].ID, start, end)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	second, err := repo.LockPeriod(ctx, budgets[0].ID, start, end)
	if err != nil {
		t.Fatalf("second lock: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("second lock made a new period %s, want %s", second.ID, first.ID)
	}
}
