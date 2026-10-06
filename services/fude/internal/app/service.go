// Package app holds the fude use cases. Each mutating use case writes its rows
// and queues its background work in one transaction.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
)

// GenerateArgs asks the queue to write one AI version of a draft.
type GenerateArgs struct {
	OwnerID uuid.UUID
	DraftID uuid.UUID
	// Version is the draft_versions number the job writes.
	Version int32
	// ExtraContext is what the owner typed when asking to regenerate.
	ExtraContext string
}

// Queue starts generation inside the caller's transaction, so a draft and its
// job commit or roll back together.
type Queue interface {
	EnqueueGenerate(ctx context.Context, tx pgx.Tx, args GenerateArgs) error
}

// Service runs the fude use cases.
type Service struct {
	pool  *pgxpool.Pool
	queue Queue
	now   func() time.Time
}

// NewService returns a Service on pool. A nil now means time.Now. A nil queue
// leaves drafts in state generating; the generation job (P7.2b) supplies one.
func NewService(pool *pgxpool.Pool, queue Queue, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, queue: queue, now: now}
}

// inTx runs fn in one transaction with a Repo bound to it.
func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx, repo *store.Repo) error) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(tx, store.New(tx))
	})
}

func (s *Service) enqueue(ctx context.Context, tx pgx.Tx, args GenerateArgs) error {
	if s.queue == nil {
		return nil
	}
	return s.queue.EnqueueGenerate(ctx, tx, args)
}

// ownerFrom returns the owner the call acts for.
func ownerFrom(ctx context.Context) (uuid.UUID, error) {
	id, ok := authz.FromContext(ctx)
	if !ok {
		return uuid.Nil, ErrNoOwner
	}
	owner, err := uuid.Parse(id.OwnerID)
	if err != nil {
		return uuid.Nil, ErrNoOwner
	}
	return owner, nil
}

// parseID reads a UUID argument.
func parseID(field, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, invalidField(field, "INVALID_ID", "%s is not a valid id", field)
	}
	return id, nil
}

// strPtr returns nil for an empty string, so empty input clears a column.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// toDomain returns the state-machine view of a draft row.
func toDomain(d db.Draft) domain.Draft {
	reason := ""
	if d.FailureReason != nil {
		reason = *d.FailureReason
	}
	return domain.Draft{
		ID: d.ID.String(), State: domain.DraftState(d.State), CurrentVersion: d.CurrentVersion,
		FailureReason: reason, Version: d.Version, UpdatedAt: d.UpdatedAt,
	}
}

// saveState writes the state columns of next onto row d, which must be at the
// version the caller read.
func saveState(ctx context.Context, repo *store.Repo, owner uuid.UUID, d db.Draft, next domain.Draft, version int32) (db.Draft, error) {
	return repo.UpdateDraftState(ctx, db.UpdateDraftStateParams{
		ID: d.ID, OwnerID: owner, State: string(next.State), CurrentVersion: next.CurrentVersion,
		FailureReason: strPtr(next.FailureReason), Version: version,
	})
}
