package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/0xHoaxen/shogun/services/fude/internal/store"
)

type fakeSampleEmbedder struct {
	err error
	ids []uuid.UUID
}

func (f *fakeSampleEmbedder) EmbedSample(_ context.Context, id uuid.UUID) error {
	f.ids = append(f.ids, id)
	return f.err
}

func TestEmbedWorker(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCancel bool
		wantErr    bool
	}{
		{"success", nil, false, false},
		{"sample is gone", store.ErrNotFound, true, true},
		{"provider error retries", errors.New("provider down"), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeSampleEmbedder{err: tt.err}
			w := &embedWorker{embedder: f, log: slog.New(slog.DiscardHandler)}
			job := &river.Job[EmbedArgs]{JobRow: &rivertype.JobRow{Attempt: 1}, Args: EmbedArgs{SampleID: uuid.New()}}

			err := w.Work(context.Background(), job)

			var cancel *rivertype.JobCancelError
			if (err != nil) != tt.wantErr || errors.As(err, &cancel) != tt.wantCancel || len(f.ids) != 1 || f.ids[0] != job.Args.SampleID {
				t.Fatalf("err %v, calls %v", err, f.ids)
			}
		})
	}
}

func TestEmbedJobIsQueuedOnceWithThreeTries(t *testing.T) {
	opts := EmbedArgs{}.InsertOpts()

	if opts.Queue != Queue || opts.MaxAttempts != 3 || !opts.UniqueOpts.ByArgs {
		t.Fatalf("got %+v", opts)
	}
}
