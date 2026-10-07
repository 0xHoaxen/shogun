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
	"github.com/0xHoaxen/shogun/services/soroban/internal/metrics"
	"github.com/0xHoaxen/shogun/services/soroban/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

// newPool is a migrated schema, which includes the seeded default budgets.
func newPool(t *testing.T) *pgxpool.Pool {
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
	return pool
}

// budget describes a budget of owner; spent is the spend in the period given by
// periodFrom and periodTo (offsets from now), or no period when spent is nil.
type budget struct {
	owner       uuid.UUID
	scopeType   string
	scopeValue  string
	period      string
	limit       int64
	enabled     bool
	spent       *int64
	periodFrom  time.Duration
	periodUntil time.Duration
}

func micros(n int64) *int64 { return &n }

func addBudget(t *testing.T, pool *pgxpool.Pool, b budget) {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO budgets (id, owner_id, scope_type, scope_value, period, limit_micros, mode, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, 'hard', $7)`,
		id, b.owner, b.scopeType, b.scopeValue, b.period, b.limit, b.enabled); err != nil {
		t.Fatalf("insert budget: %v", err)
	}
	if b.spent == nil {
		return
	}
	now := time.Now()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO budget_periods (id, budget_id, period_start, period_end, spent_micros)
		VALUES ($1, $2, $3, $4, $5)`,
		uuid.New(), id, now.Add(b.periodFrom), now.Add(b.periodUntil), *b.spent); err != nil {
		t.Fatalf("insert period: %v", err)
	}
}

// monthly is a budget of limit micro-dollars whose current period spent the given amount.
func monthly(owner uuid.UUID, scopeValue string, limit int64, spent *int64) budget {
	return budget{
		owner: owner, scopeType: "service", scopeValue: scopeValue, period: "monthly", limit: limit, enabled: true,
		spent: spent, periodFrom: -24 * time.Hour, periodUntil: 24 * time.Hour,
	}
}

func used(t *testing.T, pool *pgxpool.Pool) map[string]float64 {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(metrics.NewBudgetUsed(pool, slog.New(slog.DiscardHandler)))
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	got := map[string]float64{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			key := ""
			for _, l := range m.GetLabel() {
				key += l.GetName() + "=" + l.GetValue() + ","
			}
			got[key] = m.GetGauge().GetValue()
		}
	}
	return got
}

func TestBudgetUsedHasNoSeriesForTheSeededTemplates(t *testing.T) {
	if got := used(t, newPool(t)); len(got) != 0 {
		t.Errorf("series = %v on a fresh database, want none", got)
	}
}

func TestBudgetUsedIsSpendOverLimitInTheCurrentPeriod(t *testing.T) {
	// Arrange
	pool := newPool(t)
	addBudget(t, pool, monthly(uuid.New(), "fude", 1000, micros(800)))

	// Act
	got := used(t, pool)

	// Assert
	if v, ok := got["period=monthly,scope_type=service,scope_value=fude,"]; !ok || v != 0.8 {
		t.Errorf("series = %v, want fude monthly at 0.8", got)
	}
}

func TestBudgetUsedIsZeroBeforeAPeriodExistsAndIgnoresExpiredPeriods(t *testing.T) {
	// Arrange: one budget never used, one whose only period ended yesterday.
	pool := newPool(t)
	owner := uuid.New()
	addBudget(t, pool, monthly(owner, "tsubame", 1000, nil))
	expired := monthly(owner, "katana", 1000, micros(900))
	expired.periodFrom, expired.periodUntil = -72*time.Hour, -24*time.Hour
	addBudget(t, pool, expired)

	// Act
	got := used(t, pool)

	// Assert
	for _, scope := range []string{"tsubame", "katana"} {
		if v, ok := got["period=monthly,scope_type=service,scope_value="+scope+","]; !ok || v != 0 {
			t.Errorf("%s = %v (present %v), want 0", scope, v, ok)
		}
	}
}

func TestBudgetUsedLeavesOutDisabledAndZeroLimitBudgets(t *testing.T) {
	// Arrange
	pool := newPool(t)
	owner := uuid.New()
	off := monthly(owner, "fude", 1000, micros(500))
	off.enabled = false
	addBudget(t, pool, off)
	addBudget(t, pool, monthly(owner, "shinobi", 0, micros(500)))

	// Act and assert
	if got := used(t, pool); len(got) != 0 {
		t.Errorf("series = %v, want none for a disabled and a zero-limit budget", got)
	}
}

func TestBudgetUsedReportsOneSeriesWhenTwoOwnersShareAScope(t *testing.T) {
	// Arrange: the scrape must not carry the same label set twice.
	pool := newPool(t)
	addBudget(t, pool, monthly(uuid.New(), "fude", 1000, micros(100)))
	addBudget(t, pool, monthly(uuid.New(), "fude", 1000, micros(600)))

	// Act
	got := used(t, pool)

	// Assert
	if v := got["period=monthly,scope_type=service,scope_value=fude,"]; len(got) != 1 || v != 0.6 {
		t.Errorf("series = %v, want one at the highest ratio 0.6", got)
	}
}
