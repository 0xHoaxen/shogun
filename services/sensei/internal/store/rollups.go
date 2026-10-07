package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store/db"
)

// RebuildRollups replaces every rollup from fromDay on with what the facts add
// up to. Build the Repo on a transaction so a failure leaves the old rollups.
// Running it twice, or after a redelivered event, changes nothing.
func (r *Repo) RebuildRollups(ctx context.Context, fromDay time.Time) error {
	steps := []struct {
		name string
		run  func(context.Context, time.Time) error
	}{
		{"delete rollups", r.q.DeleteRollupsFrom},
		{"roll up jobs added", r.q.RollupJobsAdded},
		{"roll up job stages", r.q.RollupJobStages},
		{"roll up contacts", r.q.RollupContacts},
		{"roll up contact moves", r.q.RollupContactMoves},
	}
	for _, step := range steps {
		if err := step.run(ctx, fromDay); err != nil {
			return wrap(step.name, err)
		}
	}
	return nil
}

// ListRollups returns the owner's rollups of the metrics between two days,
// both included.
func (r *Repo) ListRollups(ctx context.Context, owner uuid.UUID, from, to time.Time, metrics []string) ([]domain.Rollup, error) {
	rows, err := r.q.ListRollups(ctx, db.ListRollupsParams{OwnerID: owner, FromDay: from, ToDay: to, Metrics: metrics})
	if err != nil {
		return nil, wrap("list rollups", err)
	}
	out := make([]domain.Rollup, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Rollup{Day: row.Day, Metric: row.Metric, Dimension: row.Dimension, Value: row.Value})
	}
	return out, nil
}
