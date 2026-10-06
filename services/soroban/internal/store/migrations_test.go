package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/soroban/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func TestMigrationsSeedPricesAndDefaultBudgets(t *testing.T) {
	// Arrange
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "soroban")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// Act
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Assert
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"seeded prices", `SELECT count(*) FROM prices`, 3},
		{"default budgets", `SELECT count(*) FROM budgets`, 11},
		{"global monthly hard budget is $20", `SELECT count(*) FROM budgets WHERE scope_type = 'global' AND period = 'monthly' AND mode = 'hard' AND limit_micros = 20000000`, 1},
		{"every feature budget is daily soft", `SELECT count(*) FROM budgets WHERE scope_type = 'feature' AND period = 'daily' AND mode = 'soft'`, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got int
			if err := pool.QueryRow(ctx, tt.query).Scan(&got); err != nil {
				t.Fatalf("query: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}
