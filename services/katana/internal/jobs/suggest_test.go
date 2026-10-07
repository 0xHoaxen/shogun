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

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
)

type fakeSuggester struct {
	err   error
	input app.SuggestInput
}

func (f *fakeSuggester) Suggest(_ context.Context, in app.SuggestInput) (int, error) {
	f.input = in
	return 2, f.err
}

var jobNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func workSuggest(t *testing.T, s Suggester) error {
	t.Helper()
	w := &suggestWorker{suggester: s, now: func() time.Time { return jobNow }, log: slog.New(slog.DiscardHandler)}
	return w.Work(context.Background(), &river.Job[SuggestArgs]{JobRow: &rivertype.JobRow{ID: 7}, Args: SuggestArgs{OwnerID: uuid.New()}})
}

func TestSuggestWorkerPassesTheRunOnAndSucceeds(t *testing.T) {
	s := &fakeSuggester{}

	err := workSuggest(t, s)

	if err != nil || s.input.OwnerID == uuid.Nil {
		t.Fatalf("err %v, input %+v", err, s.input)
	}
}

func TestSuggestWorkerCancelsWhenSuggestionsAreOff(t *testing.T) {
	err := workSuggest(t, &fakeSuggester{err: app.ErrSuggestOff})

	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) {
		t.Fatalf("err = %v, want a cancel, since a retry cannot help", err)
	}
}

func TestSuggestWorkerSnoozesUntilTheBudgetResets(t *testing.T) {
	tests := []struct {
		name     string
		resetsAt time.Time
		want     time.Duration
	}{
		{"known reset", jobNow.Add(5 * time.Hour), 5 * time.Hour},
		{"unknown reset", time.Time{}, minBudgetSnooze},
		{"reset already past", jobNow.Add(-time.Hour), time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := workSuggest(t, &fakeSuggester{err: errors.Join(errors.New("ask"), &llm.BudgetError{ResetsAt: tt.resetsAt})})

			var snooze *river.JobSnoozeError
			if !errors.As(err, &snooze) || snooze.Duration != tt.want {
				t.Fatalf("err = %v, want a snooze of %s", err, tt.want)
			}
		})
	}
}

func TestSuggestWorkerRetriesOtherFailures(t *testing.T) {
	boom := errors.New("model down")

	err := workSuggest(t, &fakeSuggester{err: boom})

	var cancel *river.JobCancelError
	var snooze *river.JobSnoozeError
	if !errors.Is(err, boom) || errors.As(err, &cancel) || errors.As(err, &snooze) {
		t.Fatalf("err = %v, want the failure returned for River to retry", err)
	}
}
