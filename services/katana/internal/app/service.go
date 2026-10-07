package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

// ErrNoOwner means the call carries no identity, or one whose owner is not a
// UUID. Transport maps it to PermissionDenied.
var ErrNoOwner = errors.New("app: call has no valid owner")

// GitHub reads the owner's activity. Fetch returns domain.ErrNotModified when
// nothing changed since etag.
type GitHub interface {
	Fetch(ctx context.Context, etag string) (domain.Snapshot, error)
}

// Service runs the katana use cases.
type Service struct {
	pool   *pgxpool.Pool
	github GitHub
	now    func() time.Time
}

// NewService returns a Service on pool. github may be nil when no GitHub user
// and token are configured; syncing then fails with domain.ErrNotConfigured.
// now is the clock; nil means time.Now.
func NewService(pool *pgxpool.Pool, github GitHub, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, github: github, now: now}
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
