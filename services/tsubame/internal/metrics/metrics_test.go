package metrics_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/metrics"
	"github.com/0xHoaxen/shogun/services/tsubame/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "tsubame")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

// addAccount stores a mailbox created createdAgo ago and last synced syncedAgo
// ago (zero means never).
func addAccount(t *testing.T, pool *pgxpool.Pool, status string, createdAgo, syncedAgo time.Duration) {
	t.Helper()
	var synced *time.Time
	if syncedAgo > 0 {
		at := time.Now().Add(-syncedAgo)
		synced = &at
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO accounts (id, owner_id, provider, address, token_ciphertext, token_key_id, status, last_synced_at, created_at)
		VALUES ($1, $2, 'gmail', $3, '\x00', 'k1', $4, $5, now() - $6::interval)`,
		uuid.New(), uuid.New(), uuid.NewString()+"@example.com", status, synced, createdAgo.String())
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
}

func syncAge(t *testing.T, pool *pgxpool.Pool) float64 {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(metrics.NewSyncAge(pool, slog.New(slog.DiscardHandler)))
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() == "shogun_gmail_sync_age_seconds" {
			return f.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatal("shogun_gmail_sync_age_seconds missing from the scrape")
	return 0
}

func TestSyncAgeIsZeroWithoutAMailbox(t *testing.T) {
	if got := syncAge(t, newPool(t)); got != 0 {
		t.Errorf("age = %v with no mailbox, want 0", got)
	}
}

func TestSyncAgeIsTimeSinceTheLastSync(t *testing.T) {
	// Arrange
	pool := newPool(t)
	addAccount(t, pool, "active", 48*time.Hour, 2*time.Hour)

	// Act and assert
	if got := syncAge(t, pool); got < 7200 || got > 7260 {
		t.Errorf("age = %v, want about 7200", got)
	}
}

func TestSyncAgeOfAMailboxNeverSyncedIsItsOwnAge(t *testing.T) {
	// Arrange
	pool := newPool(t)
	addAccount(t, pool, "active", 10*time.Minute, 0)

	// Act and assert
	if got := syncAge(t, pool); got < 600 || got > 660 {
		t.Errorf("age = %v, want about 600", got)
	}
}

func TestSyncAgeIsTheStalestMailboxAndIgnoresDisabledOnes(t *testing.T) {
	// Arrange: a fresh one, a stale one that needs a new login, and a disabled old one.
	pool := newPool(t)
	addAccount(t, pool, "active", 48*time.Hour, time.Minute)
	addAccount(t, pool, "reauth_required", 48*time.Hour, 3*time.Hour)
	addAccount(t, pool, "disabled", 48*time.Hour, 24*time.Hour)

	// Act and assert
	if got := syncAge(t, pool); got < 10800 || got > 10860 {
		t.Errorf("age = %v, want about 10800 (the reauth_required mailbox)", got)
	}
}
