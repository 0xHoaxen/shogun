// Package app holds the soroban use cases. Each mutating use case writes its
// rows and its outbox event in one transaction.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
)

const (
	// eventSource is the source name on every event soroban produces.
	eventSource = "soroban"

	eventBudgetExhausted = "cost.budget_exhausted"
)

// Service runs the soroban use cases.
type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
	loc  *time.Location
}

// NewService returns a Service on pool. A nil now means time.Now.
func NewService(pool *pgxpool.Pool, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, now: now, loc: domain.Location()}
}

// inTx runs fn in one transaction with a Repo bound to it.
func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx, repo *store.Repo) error) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(tx, store.New(tx))
	})
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
