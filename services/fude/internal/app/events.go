package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
)

// eventKeyPrefix starts the idempotency key of a draft created for an event.
const eventKeyPrefix = "event:"

// EventDraftInput describes a draft an event asks for.
type EventDraftInput struct {
	OwnerID    uuid.UUID
	Kind       domain.Kind
	TargetType domain.TargetType
	TargetID   uuid.UUID
	Channel    domain.Channel
	// EventID is the id of the event, which makes a redelivery a no-op.
	EventID string
}

// DraftFromEvent creates a draft in state generating and queues its first
// version, inside tx, which is the event's inbox transaction. An email draft
// has no recipient yet; generation takes it from the contact. A second call for
// the same event creates nothing.
func (s *Service) DraftFromEvent(ctx context.Context, tx pgx.Tx, in EventDraftInput) error {
	switch {
	case in.OwnerID == uuid.Nil:
		return fmt.Errorf("%w: no owner", ErrInvalidEvent)
	case in.EventID == "":
		return fmt.Errorf("%w: no event id", ErrInvalidEvent)
	case !in.Kind.Valid() || !in.TargetType.Valid() || !in.Channel.Valid() || in.TargetID == uuid.Nil:
		return fmt.Errorf("%w: kind, target or channel", ErrInvalidEvent)
	}
	repo := store.New(tx)
	key := eventKeyPrefix + in.EventID
	if _, err := repo.GetDraftByIdempotencyKey(ctx, in.OwnerID, key); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	d, err := repo.InsertDraft(ctx, db.InsertDraftParams{
		ID: store.NewID(), OwnerID: in.OwnerID, Kind: string(in.Kind), TargetType: string(in.TargetType),
		TargetID: &in.TargetID, Channel: string(in.Channel), IdempotencyKey: &key,
	})
	if err != nil {
		return err
	}
	return s.enqueue(ctx, tx, GenerateArgs{OwnerID: in.OwnerID, DraftID: d.ID, Version: 1})
}

// ErrInvalidEvent means an event cannot be turned into a draft however often
// it is retried.
var ErrInvalidEvent = errors.New("app: invalid event")
