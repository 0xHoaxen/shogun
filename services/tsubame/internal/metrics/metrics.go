// Package metrics exposes tsubame's own Prometheus metrics.
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

// syncAgeSQL is the age in seconds of the stalest mailbox that should be
// syncing. An account never synced is as old as the account. The sync runs
// every five minutes, so an age of an hour means the sync is failing. Nothing
// here names an address.
const syncAgeSQL = `
SELECT COALESCE(max(EXTRACT(EPOCH FROM now() - COALESCE(last_synced_at, created_at))), 0)::float8
  FROM accounts WHERE status <> 'disabled'`

// SyncAge reports how long ago the stalest mailbox was last synced.
type SyncAge struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	desc *prometheus.Desc
}

// NewSyncAge returns a collector that queries pool on each scrape. A failing
// query leaves the metric out of the scrape instead of failing it.
func NewSyncAge(pool *pgxpool.Pool, log *slog.Logger) *SyncAge {
	return &SyncAge{
		pool: pool,
		log:  log,
		desc: prometheus.NewDesc("shogun_gmail_sync_age_seconds",
			"Seconds since the stalest connected mailbox was last synced; 0 with no mailbox.", nil, nil),
	}
}

// Describe implements prometheus.Collector.
func (s *SyncAge) Describe(ch chan<- *prometheus.Desc) { ch <- s.desc }

// Collect implements prometheus.Collector.
func (s *SyncAge) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	var seconds float64
	if err := s.pool.QueryRow(ctx, syncAgeSQL).Scan(&seconds); err != nil {
		s.log.Warn("metrics: gmail sync age", slog.Any("error", err))
		return
	}
	ch <- prometheus.MustNewConstMetric(s.desc, prometheus.GaugeValue, seconds)
}
