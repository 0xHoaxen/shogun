package relay

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// scrapeTimeout bounds the queries one /metrics scrape runs.
	scrapeTimeout = 3 * time.Second
	// discardedWindow is how far back discarded jobs are counted.
	discardedWindow = "24 hours"
)

// lagSQL is the age in seconds of the oldest event some consumer has not
// acknowledged: an outbox row not yet relayed, or a deliver job still waiting,
// retrying or running. The relay marks an outbox row delivered once its deliver
// jobs are queued, so the outbox alone would not grow when a consumer is down.
const lagSQL = `
SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8
  FROM (SELECT created_at FROM outbox WHERE delivered_at IS NULL
        UNION ALL
        SELECT created_at FROM river_job
         WHERE kind = $1 AND state IN ('available', 'pending', 'retryable', 'running', 'scheduled')) waiting`

const discardedSQL = `
SELECT kind, count(*) FROM river_job
 WHERE state = 'discarded' AND finalized_at > now() - interval '` + discardedWindow + `'
 GROUP BY kind`

// Metrics describes the relay's backlog of one service schema.
type Metrics struct {
	pool      *pgxpool.Pool
	log       *slog.Logger
	lag       *prometheus.Desc
	discarded *prometheus.Desc
}

// NewMetrics returns a collector of outbox lag and discarded jobs for the
// schema pool is pinned to. It queries on each scrape; a failing query leaves
// that metric out of the scrape instead of failing it. A nil log uses slog's
// default.
func NewMetrics(pool *pgxpool.Pool, log *slog.Logger) *Metrics {
	if log == nil {
		log = slog.Default()
	}
	return &Metrics{
		pool: pool,
		log:  log.With("component", "relay"),
		lag: prometheus.NewDesc("shogun_outbox_lag_seconds",
			"Age of the oldest event not yet acknowledged by every consumer.", nil, nil),
		discarded: prometheus.NewDesc("shogun_river_jobs_discarded",
			"River jobs discarded in the last "+discardedWindow+", by kind.", []string{"kind"}, nil),
	}
}

// Describe implements prometheus.Collector.
func (m *Metrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.lag
	ch <- m.discarded
}

// Collect implements prometheus.Collector.
func (m *Metrics) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	m.collectLag(ctx, ch)
	m.collectDiscarded(ctx, ch)
}

func (m *Metrics) collectLag(ctx context.Context, ch chan<- prometheus.Metric) {
	var seconds float64
	if err := m.pool.QueryRow(ctx, lagSQL, kindDeliver).Scan(&seconds); err != nil {
		m.log.Warn("metrics: outbox lag", "error", err)
		return
	}
	ch <- prometheus.MustNewConstMetric(m.lag, prometheus.GaugeValue, seconds)
}

func (m *Metrics) collectDiscarded(ctx context.Context, ch chan<- prometheus.Metric) {
	rows, err := m.pool.Query(ctx, discardedSQL)
	if err != nil {
		m.log.Warn("metrics: discarded jobs", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			kind  string
			count int64
		)
		if err := rows.Scan(&kind, &count); err != nil {
			m.log.Warn("metrics: discarded jobs", "error", err)
			return
		}
		ch <- prometheus.MustNewConstMetric(m.discarded, prometheus.GaugeValue, float64(count), kind)
	}
	if err := rows.Err(); err != nil {
		m.log.Warn("metrics: discarded jobs", "error", err)
	}
}
