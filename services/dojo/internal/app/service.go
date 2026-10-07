package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
)

const (
	eventSource        = "dojo"
	eventActivityAdded = "learning.activity_added"
	eventItemCompleted = "learning.item_completed"

	// istOffset is the offset of Asia/Kolkata, the zone the owner's days are
	// counted in. India has no daylight saving time, so a fixed offset is exact.
	istOffset = 5*time.Hour + 30*time.Minute
)

// Drafter is the part of fude's client that GeneratePost uses.
type Drafter interface {
	GenerateDraft(ctx context.Context, in *fudev1.GenerateDraftRequest, opts ...grpc.CallOption) (*fudev1.GenerateDraftResponse, error)
}

// Service runs the learning log use cases.
type Service struct {
	pool *pgxpool.Pool
	fude Drafter
	now  func() time.Time
}

// NewService returns a Service on pool that asks fude for post drafts. now is
// the clock; nil means time.Now.
func NewService(pool *pgxpool.Pool, fude Drafter, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, fude: fude, now: now}
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

// today is the current date in the owner's zone, as midnight UTC, which is how
// a date column reads back.
func (s *Service) today() time.Time {
	local := s.now().UTC().Add(istOffset)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}
