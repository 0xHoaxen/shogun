package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
)

// VectorLiteral formats an embedding as the text pgvector parses, such as
// "[0.1,0.2]".
func VectorLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strconv.FormatFloat(float64(f), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// ListSimilarVoiceSamples returns up to limit of the owner's samples for a
// channel, closest to query first. Samples not embedded yet follow, newest
// first.
func (r *Repo) ListSimilarVoiceSamples(ctx context.Context, owner uuid.UUID, channel string, query []float32, limit int32) ([]db.ListSimilarVoiceSamplesRow, error) {
	literal := VectorLiteral(query)
	rows, err := r.q.ListSimilarVoiceSamples(ctx, db.ListSimilarVoiceSamplesParams{
		OwnerID: owner, Channel: channel, Query: &literal, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list similar voice samples: %w", mapErr(err))
	}
	return rows, nil
}

// GetVoiceSampleText returns the text of a sample. It is ErrNotFound when the
// sample is gone.
func (r *Repo) GetVoiceSampleText(ctx context.Context, id uuid.UUID) (string, error) {
	text, err := r.q.GetVoiceSampleText(ctx, id)
	return text, mapErr(err)
}

// SetVoiceSampleEmbedding stores a sample's embedding. It is ErrNotFound when
// the sample is gone.
func (r *Repo) SetVoiceSampleEmbedding(ctx context.Context, id uuid.UUID, embedding []float32) error {
	literal := VectorLiteral(embedding)
	n, err := r.q.SetVoiceSampleEmbedding(ctx, db.SetVoiceSampleEmbeddingParams{ID: id, Embedding: &literal})
	if err != nil {
		return fmt.Errorf("set voice sample embedding: %w", mapErr(err))
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
