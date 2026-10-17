package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store/db"
)

// RebuildRollups replaces the owner's rollups from fromDay on with what their
// facts add up to, counting days in timezone (an IANA name). Build the Repo on a
// transaction so a failure leaves the old rollups. Running it twice, or after a
// redelivered event, changes nothing, and no other owner's rollups are touched.
func (r *Repo) RebuildRollups(ctx context.Context, owner uuid.UUID, timezone string, fromDay time.Time) error {
	steps := []struct {
		name string
		run  func() error
	}{
		{"delete rollups", func() error {
			return r.q.DeleteRollupsFrom(ctx, db.DeleteRollupsFromParams{OwnerID: owner, FromDay: fromDay})
		}},
		{"roll up jobs added", func() error {
			return r.q.RollupJobsAdded(ctx, db.RollupJobsAddedParams{OwnerID: owner, Tz: timezone, FromDay: fromDay})
		}},
		{"roll up job stages", func() error {
			return r.q.RollupJobStages(ctx, db.RollupJobStagesParams{OwnerID: owner, Tz: timezone, FromDay: fromDay})
		}},
		{"roll up contacts", func() error {
			return r.q.RollupContacts(ctx, db.RollupContactsParams{OwnerID: owner, Tz: timezone, FromDay: fromDay})
		}},
		{"roll up contact moves", func() error {
			return r.q.RollupContactMoves(ctx, db.RollupContactMovesParams{OwnerID: owner, Tz: timezone, FromDay: fromDay})
		}},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
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
