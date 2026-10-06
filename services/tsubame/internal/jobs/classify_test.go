package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
)

type fakeClassifier struct {
	err      error
	calls    []uuid.UUID
	identity authz.Identity
}

func (f *fakeClassifier) Classify(ctx context.Context, _, messageID uuid.UUID) error {
	f.calls = append(f.calls, messageID)
	f.identity, _ = authz.FromContext(ctx)
	return f.err
}

var testNow = time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

func newClassifyWorker(c *fakeClassifier) *classifyWorker {
	return &classifyWorker{classifier: c, log: slog.New(slog.DiscardHandler), now: func() time.Time { return testNow }}
}

func classifyJob(attempt int) *river.Job[ClassifyArgs] {
	return &river.Job[ClassifyArgs]{
		JobRow: &rivertype.JobRow{ID: 9, Attempt: attempt, MaxAttempts: classifyAttempts},
		Args:   ClassifyArgs{OwnerID: uuid.New(), MessageID: uuid.New()},
	}
}

func TestClassifyWorkRunsAsTheOwner(t *testing.T) {
	c := &fakeClassifier{}
	job := classifyJob(1)

	err := newClassifyWorker(c).Work(context.Background(), job)

	if err != nil || len(c.calls) != 1 || c.calls[0] != job.Args.MessageID {
		t.Fatalf("err %v, calls %v", err, c.calls)
	}
	if c.identity.OwnerID != job.Args.OwnerID.String() || c.identity.RequestID == "" {
		t.Fatalf("got identity %+v, want the owner's", c.identity)
	}
}

func TestClassifyWorkRetriesOtherFailures(t *testing.T) {
	for _, attempt := range []int{1, classifyAttempts} {
		c := &fakeClassifier{err: errors.New("kagami down")}

		err := newClassifyWorker(c).Work(context.Background(), classifyJob(attempt))

		var snooze *rivertype.JobSnoozeError
		var cancel *rivertype.JobCancelError
		if err == nil || errors.As(err, &snooze) || errors.As(err, &cancel) {
			t.Fatalf("attempt %d: got %v, want a plain error River retries", attempt, err)
		}
	}
}

func TestClassifyWorkSnoozesUntilTheBudgetResets(t *testing.T) {
	tests := []struct {
		name     string
		resetsAt time.Time
		want     time.Duration
	}{
		{"until the reset", testNow.Add(3 * time.Hour), 3 * time.Hour},
		{"at least a minute", testNow.Add(time.Second), time.Minute},
		{"an hour when soroban gave no time", time.Time{}, minSnooze},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &fakeClassifier{err: &llm.BudgetError{ResetsAt: tt.resetsAt}}

			err := newClassifyWorker(c).Work(context.Background(), classifyJob(classifyAttempts))

			var snooze *rivertype.JobSnoozeError
			if !errors.As(err, &snooze) || snooze.Duration != tt.want {
				t.Fatalf("got %v, want a %s snooze", err, tt.want)
			}
		})
	}
}

func TestClassifyWorkCancelsWhenTheMessageIsGone(t *testing.T) {
	c := &fakeClassifier{err: store.ErrNotFound}

	err := newClassifyWorker(c).Work(context.Background(), classifyJob(1))

	var cancel *rivertype.JobCancelError
	if !errors.As(err, &cancel) {
		t.Fatalf("got %v", err)
	}
}

func TestClassifyJobIsQueuedOnceWithThreeTries(t *testing.T) {
	opts := ClassifyArgs{}.InsertOpts()

	if opts.Queue != Queue || opts.MaxAttempts != 3 || !opts.UniqueOpts.ByArgs {
		t.Fatalf("got %+v", opts)
	}
	if got := newClassifyWorker(&fakeClassifier{}).Timeout(nil); got != classifyTimeout {
		t.Fatalf("timeout %s", got)
	}
}
