package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/jobs"
)

type fakeScorer struct {
	err   error
	input app.ScoreInput
}

func (f *fakeScorer) ScorePosting(_ context.Context, in app.ScoreInput) error {
	f.input = in
	return f.err
}

func TestTheScoreWorkerScoresThePostingItWasQueuedFor(t *testing.T) {
	sc := &fakeScorer{}
	args := jobs.ScorePostingArgs{OwnerID: uuid.New(), PostingID: uuid.New(), Version: 7}

	err := jobs.WorkScore(context.Background(), sc, clock, args)

	if err != nil || sc.input != (app.ScoreInput{OwnerID: args.OwnerID, PostingID: args.PostingID, Version: 7}) {
		t.Fatalf("err %v, input %+v", err, sc.input)
	}
}

func TestTheScoreWorkerSnoozesUntilTheBudgetResets(t *testing.T) {
	tests := []struct {
		name     string
		resetsAt time.Time
		want     time.Duration
	}{
		{"known reset", jobNow.Add(5 * time.Hour), 5 * time.Hour},
		{"unknown reset", time.Time{}, time.Hour},
		{"reset already past", jobNow.Add(-time.Hour), time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := jobs.WorkScore(context.Background(), &fakeScorer{err: errors.Join(errors.New("ask"), &llm.BudgetError{ResetsAt: tt.resetsAt})}, clock, jobs.ScorePostingArgs{})

			var snooze *river.JobSnoozeError
			if !errors.As(err, &snooze) || snooze.Duration != tt.want {
				t.Fatalf("err = %v, want a snooze of %s", err, tt.want)
			}
		})
	}
}

func TestTheScoreWorkerRetriesOtherFailures(t *testing.T) {
	boom := errors.New("model down")

	err := jobs.WorkScore(context.Background(), &fakeScorer{err: boom}, clock, jobs.ScorePostingArgs{})

	var snooze *river.JobSnoozeError
	if !errors.Is(err, boom) || errors.As(err, &snooze) {
		t.Fatalf("err = %v, want the failure returned for River to retry", err)
	}
}

func TestRiverQueueStartsOneScoreJobPerPostingAndVersionInTheCallersTransaction(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	queue, err := jobs.NewRiverQueue(pool)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	owner, posting := uuid.New(), uuid.New()
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'score_posting'`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	enqueue := func(version int64, commit bool) {
		_ = postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
			if err := queue.EnqueueScore(ctx, tx, app.ScoreInput{OwnerID: owner, PostingID: posting, Version: version}); err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			if !commit {
				return context.Canceled
			}
			return nil
		})
	}

	enqueue(0, false)
	afterRollback := count()
	enqueue(0, true)
	enqueue(0, true)
	afterSameVersion := count()
	enqueue(5, true)

	if afterRollback != 0 || afterSameVersion != 1 || count() != 2 {
		t.Fatalf("jobs: after rollback %d, same version twice %d, a new version %d; want 0, 1, 2", afterRollback, afterSameVersion, count())
	}
}
