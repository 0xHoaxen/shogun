package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

// SuggestInput says what a suggestion run is about. A run is started by a new
// snapshot, or by something the owner finished learning, or both.
type SuggestInput struct {
	OwnerID uuid.UUID
	// SnapshotID is the snapshot that just arrived; nil when a learned item
	// started the run, which then looks at the latest one.
	SnapshotID *uuid.UUID
	Learned    *domain.LearnedItem
}

// Queue inserts suggest jobs inside the caller's transaction, so the change
// that calls for a run and the run itself succeed or fail together.
type Queue interface {
	EnqueueSuggest(ctx context.Context, tx pgx.Tx, in SuggestInput) error
}
