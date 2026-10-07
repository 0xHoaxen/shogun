// Package metrics exposes soroban's own Prometheus metrics.
package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// scrapeTimeout bounds the query one /metrics scrape runs.
const scrapeTimeout = 3 * time.Second

// templateOwner owns the default budgets that are copied to a real owner on
// first use; they are not budgets anything spends against.
const templateOwner = "00000000-0000-0000-0000-000000000000"

// usedSQL is spend over limit for each enabled budget in its current period. A
// budget with a limit of zero has no ratio and is left out; a budget with no
// period yet has spent nothing. The highest ratio wins per label set, so a
// scrape can never carry the same series twice.
const usedSQL = `
SELECT b.scope_type, b.scope_value, b.period,
       max(COALESCE(p.spent_micros, 0)::float8 / b.limit_micros)
  FROM budgets b
  LEFT JOIN budget_periods p ON p.budget_id = b.id AND p.period_start <= now() AND p.period_end > now()
 WHERE b.enabled AND b.limit_micros > 0 AND b.owner_id <> '` + templateOwner + `'
 GROUP BY b.scope_type, b.scope_value, b.period`

// BudgetUsed reports how much of each budget the current period has spent.
type BudgetUsed struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	desc *prometheus.Desc
}

// NewBudgetUsed returns a collector that queries pool on each scrape. A failing
// query leaves the metric out of the scrape instead of failing it.
func NewBudgetUsed(pool *pgxpool.Pool, log *slog.Logger) *BudgetUsed {
	return &BudgetUsed{
		pool: pool,
		log:  log,
		desc: prometheus.NewDesc("shogun_budget_used_ratio",
			"Spend over limit of an enabled budget in its current period (1 is the limit).",
			[]string{"scope_type", "scope_value", "period"}, nil),
	}
}

// Describe implements prometheus.Collector.
func (b *BudgetUsed) Describe(ch chan<- *prometheus.Desc) { ch <- b.desc }

// Collect implements prometheus.Collector.
func (b *BudgetUsed) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	rows, err := b.pool.Query(ctx, usedSQL)
	if err != nil {
		b.log.Warn("metrics: budget used", slog.Any("error", err))
		return
	}
	defer rows.Close()
	var metrics []prometheus.Metric
	for rows.Next() {
		var scopeType, scopeValue, period string
		var ratio float64
		if err := rows.Scan(&scopeType, &scopeValue, &period, &ratio); err != nil {
			b.log.Warn("metrics: budget used", slog.Any("error", err))
			return
		}
		metrics = append(metrics, prometheus.MustNewConstMetric(b.desc, prometheus.GaugeValue, ratio, scopeType, scopeValue, period))
	}
	if err := rows.Err(); err != nil {
		b.log.Warn("metrics: budget used", slog.Any("error", err))
		return
	}
	for _, m := range metrics {
		ch <- m
	}
}
