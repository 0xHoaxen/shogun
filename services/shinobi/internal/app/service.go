package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
)

// Fetcher reads a source's address and returns what it answered.
type Fetcher interface {
	Get(ctx context.Context, rawURL string) ([]byte, error)
}

// Service runs the shinobi use cases.
type Service struct {
	pool    *pgxpool.Pool
	fetcher Fetcher
	now     func() time.Time
	log     *slog.Logger

	// Set by WithScoring.
	queue Queue
	llm   Completer

	// Set by WithTracker.
	tracker Tracker
}

// NewService returns a Service on pool that reads sources through fetcher. now
// is the clock; nil means time.Now.
func NewService(pool *pgxpool.Pool, fetcher Fetcher, now func() time.Time, log *slog.Logger, opts ...Option) *Service {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Service{pool: pool, fetcher: fetcher, now: now, log: log}
	for _, opt := range opts {
		opt(s)
	}
	return s
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
