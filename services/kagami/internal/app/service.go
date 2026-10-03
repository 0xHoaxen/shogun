// Package app holds the kagami use cases. Each mutating use case writes its
// rows and its outbox event in one transaction.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
)

const (
	// eventSource is the source name on every event kagami produces.
	eventSource = "kagami"
	// dateLayout is the wire format of dates without a time.
	dateLayout = "2006-01-02"
)

// Service runs the kagami use cases.
type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewService returns a Service on pool. A nil now means time.Now.
func NewService(pool *pgxpool.Pool, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, now: now}
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

// today returns the current UTC date at midnight.
func (s *Service) today() time.Time {
	y, m, d := s.now().UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// parseID reads a UUID argument.
func parseID(field, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, invalidField(field, "INVALID_ID", "%s is not a valid id", field)
	}
	return id, nil
}

// parseDate reads a YYYY-MM-DD argument. Empty means no date.
func parseDate(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(dateLayout, value)
	if err != nil {
		return nil, invalidField(field, "INVALID_DATE", "%s must be YYYY-MM-DD", field)
	}
	return &t, nil
}

// strPtr returns nil for an empty string, so empty input clears a column.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// jsonObject marshals an event payload; the maps passed in cannot fail.
func jsonObject(fields map[string]any) []byte {
	raw, err := json.Marshal(fields)
	if err != nil {
		panic(fmt.Sprintf("app: marshal payload: %v", err))
	}
	return raw
}
