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
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
)

type fakeDrafter struct {
	generateErr, failErr error
	generated            []app.GenerateArgs
	failed               []string
	identity             authz.Identity
}

func (f *fakeDrafter) Generate(ctx context.Context, args app.GenerateArgs) error {
	f.generated = append(f.generated, args)
	f.identity, _ = authz.FromContext(ctx)
	return f.generateErr
}

func (f *fakeDrafter) Fail(_ context.Context, _ app.GenerateArgs, reason string) error {
	f.failed = append(f.failed, reason)
	return f.failErr
}

var testNow = time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

func newWorker(d *fakeDrafter) *generateWorker {
	return &generateWorker{drafter: d, log: slog.New(slog.DiscardHandler), now: func() time.Time { return testNow }}
}

func jobAt(attempt int) *river.Job[GenerateArgs] {
	return &river.Job[GenerateArgs]{
		JobRow: &rivertype.JobRow{ID: 7, Attempt: attempt, MaxAttempts: maxAttempts},
		Args:   GenerateArgs{OwnerID: uuid.New(), DraftID: uuid.New(), Version: 2, ExtraContext: "shorter"},
	}
}

func TestWorkRunsGenerateAsTheOwner(t *testing.T) {
	d := &fakeDrafter{}
	job := jobAt(1)

	err := newWorker(d).Work(context.Background(), job)

	if err != nil || len(d.generated) != 1 {
		t.Fatalf("err %v, calls %d", err, len(d.generated))
	}
	got := d.generated[0]
	if got.OwnerID != job.Args.OwnerID || got.DraftID != job.Args.DraftID || got.Version != 2 || got.ExtraContext != "shorter" {
		t.Fatalf("got args %+v", got)
	}
	if d.identity.OwnerID != job.Args.OwnerID.String() || d.identity.RequestID == "" {
		t.Fatalf("got identity %+v, want the owner's", d.identity)
	}
}

func TestWorkRetriesUntilTheLastAttemptThenFailsTheDraft(t *testing.T) {
	boom := errors.New("model down")
	tests := []struct {
		name       string
		attempt    int
		failErr    error
		wantErr    bool
		wantFailed int
	}{
		{"first attempt retries", 1, nil, true, 0},
		{"fourth attempt retries", maxAttempts - 1, nil, true, 0},
		{"last attempt fails the draft and ends", maxAttempts, nil, false, 1},
		{"last attempt reports a failure that cannot be recorded", maxAttempts, errors.New("db down"), true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &fakeDrafter{generateErr: boom, failErr: tt.failErr}

			err := newWorker(d).Work(context.Background(), jobAt(tt.attempt))

			if (err != nil) != tt.wantErr || len(d.failed) != tt.wantFailed {
				t.Fatalf("err %v, failed %v", err, d.failed)
			}
			if tt.wantFailed == 1 && d.failed[0] != app.ReasonGenerationFailed {
				t.Fatalf("got reason %q", d.failed[0])
			}
		})
	}
}

func TestWorkRecordsTheReasonOfTheFinalFailure(t *testing.T) {
	d := &fakeDrafter{generateErr: llm.ErrRefused}

	err := newWorker(d).Work(context.Background(), jobAt(maxAttempts))

	if err != nil || len(d.failed) != 1 || d.failed[0] != app.ReasonModelRefused {
		t.Fatalf("err %v, failed %v", err, d.failed)
	}
}

func TestWorkSnoozesUntilTheBudgetResetsWithoutFailingTheDraft(t *testing.T) {
	tests := []struct {
		name     string
		resetsAt time.Time
		want     time.Duration
	}{
		{"until the reset", testNow.Add(5 * time.Hour), 5 * time.Hour},
		{"at least a minute", testNow.Add(time.Second), time.Minute},
		{"an hour when soroban gave no time", time.Time{}, minSnooze},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &fakeDrafter{generateErr: &llm.BudgetError{ResetsAt: tt.resetsAt}}

			err := newWorker(d).Work(context.Background(), jobAt(maxAttempts))

			var snooze *rivertype.JobSnoozeError
			if !errors.As(err, &snooze) || snooze.Duration != tt.want || len(d.failed) != 0 {
				t.Fatalf("err %v, failed %v; want a %s snooze and no failure", err, d.failed, tt.want)
			}
		})
	}
}

func TestWorkCancelsWhenTheDraftIsGone(t *testing.T) {
	d := &fakeDrafter{generateErr: store.ErrNotFound}

	err := newWorker(d).Work(context.Background(), jobAt(1))

	var cancel *rivertype.JobCancelError
	if !errors.As(err, &cancel) || len(d.failed) != 0 {
		t.Fatalf("err %v, failed %v", err, d.failed)
	}
}

func TestSetupRunsTwoWorkersOnItsOwnQueue(t *testing.T) {
	setup := NewSetup(&fakeDrafter{}, slog.New(slog.DiscardHandler))

	opts := GenerateArgs{}.InsertOpts()

	if setup.Workers == nil || setup.Queues[Queue].MaxWorkers != workers || opts.Queue != Queue ||
		opts.MaxAttempts != maxAttempts || !opts.UniqueOpts.ByArgs {
		t.Fatalf("got %+v, %+v", setup, opts)
	}
	if got := newWorker(&fakeDrafter{}).Timeout(nil); got != generateTimeout {
		t.Fatalf("timeout %s", got)
	}
}
