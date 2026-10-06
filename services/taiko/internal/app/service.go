package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/services/taiko/internal/domain"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
)

// Service runs the notification use cases.
type Service struct {
	pool   *pgxpool.Pool
	now    func() time.Time
	broker *broker
}

// NewService returns a Service on pool. now is the clock; nil means time.Now.
func NewService(pool *pgxpool.Pool, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, now: now, broker: newBroker()}
}

// RecordInput describes a notification caused by an event.
type RecordInput struct {
	OwnerID uuid.UUID
	// EventID is the id of the event that caused the notification. A second
	// Record with the same id stores nothing.
	EventID uuid.UUID
	Notice  domain.Notice
}

// Record stores a notification in tx, the transaction that also marks the event
// as seen, and announces it to the streams when tx commits. A repeated event
// stores and announces nothing.
func (s *Service) Record(ctx context.Context, tx pgx.Tx, in RecordInput) error {
	repo := store.New(tx)
	row, created, err := repo.Insert(ctx, store.NewNotification{
		ID: store.NewID(), OwnerID: in.OwnerID, Notice: in.Notice,
		CreatedAt: s.now().UTC(), SourceEventID: &in.EventID,
	})
	if err != nil {
		return fmt.Errorf("record notification: %w", err)
	}
	if !created {
		return nil
	}
	if err := repo.Notify(ctx, in.OwnerID, row.ID); err != nil {
		return fmt.Errorf("record notification: %w", err)
	}
	return nil
}
