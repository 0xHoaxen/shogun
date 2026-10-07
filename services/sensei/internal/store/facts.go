package store

import (
	"context"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store/db"
)

// InsertFact stores a fact. It reports false, and stores nothing, when a fact
// for the same event already exists.
func (r *Repo) InsertFact(ctx context.Context, f domain.Fact) (bool, error) {
	dim := f.Dimension
	if dim == nil {
		dim = map[string]string{}
	}
	raw, err := marshal(dim)
	if err != nil {
		return false, err
	}
	n, err := r.q.InsertFact(ctx, db.InsertFactParams{
		EventID: f.EventID, OwnerID: f.OwnerID, Type: f.Type, Dimension: raw, OccurredAt: f.OccurredAt,
	})
	return n == 1, wrap("insert fact", err)
}

// CountFacts returns how many facts of a type the owner has.
func (r *Repo) CountFacts(ctx context.Context, owner uuid.UUID, typ string) (int64, error) {
	n, err := r.q.CountFacts(ctx, db.CountFactsParams{OwnerID: owner, Type: typ})
	return n, wrap("count facts", err)
}
