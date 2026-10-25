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

type fakeReader struct{ err error }

func (f fakeReader) ExtractJobSkills(context.Context, uuid.UUID, uuid.UUID) error { return f.err }

func TestJobSkillsWorkerCancelsSnoozesOrRetries(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		err   error
		check func(t *testing.T, err error)
	}{
		{"done", nil, func(t *testing.T, err error) {
			if err != nil {
				t.Errorf("err = %v", err)
			}
		}},
		{"no provider cancels", llm.ErrProviderNotConfigured, func(t *testing.T, err error) {
			var c *river.JobCancelError
			if !errors.As(err, &c) {
				t.Errorf("err = %v, want a cancel", err)
			}
		}},
		{"katana without a model cancels", app.ErrJobSkillsOff, func(t *testing.T, err error) {
			var c *river.JobCancelError
			if !errors.As(err, &c) {
				t.Errorf("err = %v, want a cancel", err)
			}
		}},
		{"budget snoozes", &llm.BudgetError{ResetsAt: now.Add(3 * time.Hour)}, func(t *testing.T, err error) {
			var s *river.JobSnoozeError
			if !errors.As(err, &s) || s.Duration != 3*time.Hour {
				t.Errorf("err = %v, want a 3h snooze", err)
			}
		}},
		{"anything else retries", errors.New("kagami down"), func(t *testing.T, err error) {
			var c *river.JobCancelError
			var s *river.JobSnoozeError
			if err == nil || errors.As(err, &c) || errors.As(err, &s) {
				t.Errorf("err = %v, want a plain error to retry", err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &jobSkillsWorker{reader: fakeReader{tt.err}, now: func() time.Time { return now }, log: slog.New(slog.DiscardHandler)}

			err := w.Work(context.Background(), &river.Job[JobSkillsArgs]{
				JobRow: &rivertype.JobRow{ID: 1}, Args: JobSkillsArgs{OwnerID: uuid.New(), JobID: uuid.New()},
			})

			tt.check(t, err)
		})
	}
}
