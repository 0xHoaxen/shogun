package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/0xHoaxen/shogun/services/fude/internal/store"
)

const (
	kindEmbedVoiceSample = "embed_voice_sample"

	// embedAttempts is how many tries a sample's embedding gets.
	embedAttempts = 3
)

// EmbedArgs are the args of embed_voice_sample.
type EmbedArgs struct {
	SampleID uuid.UUID `json:"sample_id" river:"unique"`
}

// Kind implements river.JobArgs.
func (EmbedArgs) Kind() string { return kindEmbedVoiceSample }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (EmbedArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: embedAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// SampleEmbedder is what the worker needs from the embedding use case.
type SampleEmbedder interface {
	EmbedSample(ctx context.Context, id uuid.UUID) error
}

type embedWorker struct {
	river.WorkerDefaults[EmbedArgs]
	embedder SampleEmbedder
	log      *slog.Logger
}

// Work embeds one sample. A sample that is gone ends the job; any other error
// retries, and after the last try the sample stays unembedded, which only
// costs its ranking: it is still used as one of the newest.
func (w *embedWorker) Work(ctx context.Context, job *river.Job[EmbedArgs]) error {
	err := w.embedder.EmbedSample(ctx, job.Args.SampleID)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return river.JobCancel(fmt.Errorf("sample is gone: %w", err))
	}
	w.log.Warn("embedding failed", slog.String("sample_id", job.Args.SampleID.String()),
		slog.Int("attempt", job.Attempt), slog.Any("error", err))
	return err
}
