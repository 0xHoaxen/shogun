package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// WorkDue runs the due_sources worker on args.
func WorkDue(ctx context.Context, s Scheduler, e Enqueuer, loc *time.Location, now func() time.Time, args DueSourcesArgs) error {
	w := &dueWorker{scheduler: s, enqueuer: e, loc: loc, now: now, log: slog.New(slog.DiscardHandler)}
	return w.Work(ctx, &river.Job[DueSourcesArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: args})
}

// WorkRun runs the run_source worker on args.
func WorkRun(ctx context.Context, r Runner, args RunSourceArgs) error {
	w := &runWorker{runner: r, log: slog.New(slog.DiscardHandler)}
	return w.Work(ctx, &river.Job[RunSourceArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: args})
}

// WorkScore runs the score_posting worker on args.
func WorkScore(ctx context.Context, sc Scorer, now func() time.Time, args ScorePostingArgs) error {
	w := &scoreWorker{scorer: sc, now: now, log: slog.New(slog.DiscardHandler)}
	return w.Work(ctx, &river.Job[ScorePostingArgs]{JobRow: &rivertype.JobRow{ID: 1}, Args: args})
}
