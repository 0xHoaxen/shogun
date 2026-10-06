package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/services/fude/internal/store"
)

// EmbeddingDimensions is the width of voice_samples.embedding.
const EmbeddingDimensions = 1024

// ErrEmbeddingSize means the embedder returned a vector of the wrong width.
var ErrEmbeddingSize = errors.New("app: embedding has the wrong number of dimensions")

// Embedder turns text into a vector of EmbeddingDimensions numbers. Texts that
// mean alike must land close together.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// VoiceEmbedder computes and stores the embeddings of voice samples.
type VoiceEmbedder struct {
	pool     *pgxpool.Pool
	embedder Embedder
}

// NewVoiceEmbedder returns a VoiceEmbedder that embeds with e.
func NewVoiceEmbedder(pool *pgxpool.Pool, e Embedder) *VoiceEmbedder {
	return &VoiceEmbedder{pool: pool, embedder: e}
}

// EmbedSample embeds one sample. It is store.ErrNotFound when the sample is
// gone, which a job should not retry.
func (v *VoiceEmbedder) EmbedSample(ctx context.Context, id uuid.UUID) error {
	repo := store.New(v.pool)
	text, err := repo.GetVoiceSampleText(ctx, id)
	if err != nil {
		return err
	}
	vec, err := v.embedder.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	if len(vec) != EmbeddingDimensions {
		return fmt.Errorf("%w: got %d, want %d", ErrEmbeddingSize, len(vec), EmbeddingDimensions)
	}
	return repo.SetVoiceSampleEmbedding(ctx, id, vec)
}
