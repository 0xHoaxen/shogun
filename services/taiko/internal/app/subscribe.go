package app

import (
	"bytes"
	"context"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

// replayBatch is how many missed notifications are read at a time.
const replayBatch = 200

// Publish hands a stored notification to the open streams of its owner. The live
// feed calls it for every notification it hears about.
func (s *Service) Publish(n db.Notification) { s.broker.publish(n) }

// Reset closes every open stream so that clients reconnect and replay. The live
// feed calls it whenever its connection was lost or restored, because
// notifications announced in between were not heard.
func (s *Service) Reset() { s.broker.reset() }

// Subscribe calls send for the owner's notifications until ctx ends or send
// fails. ready is called once the stream is registered, so a caller can tell its
// client that anything created from now on will arrive. With afterID it first
// sends everything created after that id, so a client that reconnects misses
// nothing; without it only new ones are sent.
// It returns ErrStreamReset when the stream was closed behind the client's back
// and the client should reconnect.
func (s *Service) Subscribe(ctx context.Context, afterID string, ready func() error, send func(db.Notification) error) error {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return err
	}
	after, err := parseAfter(afterID)
	if err != nil {
		return err
	}

	// Subscribing before the replay closes the gap: whatever is created while
	// the replay runs is already queued, and the id check below drops repeats.
	sub, closeSub := s.broker.subscribe(owner)
	defer closeSub()
	if err := ready(); err != nil {
		return err
	}

	last := after
	if after != uuid.Nil {
		if last, err = s.replay(ctx, owner, after, send); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case n, ok := <-sub.ch:
			if !ok {
				return ErrStreamReset
			}
			if after != uuid.Nil && bytes.Compare(n.ID[:], last[:]) <= 0 {
				continue
			}
			if err := send(n); err != nil {
				return err
			}
			last = n.ID
		}
	}
}

// replay sends the notifications created after id, oldest first, and returns
// the id of the last one sent (id itself when there were none).
func (s *Service) replay(ctx context.Context, owner, id uuid.UUID, send func(db.Notification) error) (uuid.UUID, error) {
	repo := store.New(s.pool)
	last := id
	for {
		rows, err := repo.ListAfter(ctx, owner, last, replayBatch)
		if err != nil {
			return uuid.Nil, err
		}
		for _, n := range rows {
			if err := send(n); err != nil {
				return uuid.Nil, err
			}
			last = n.ID
		}
		if len(rows) < replayBatch {
			return last, nil
		}
	}
}

func parseAfter(value string) (uuid.UUID, error) {
	if value == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, &InvalidArgumentError{Reason: "INVALID_ID", Msg: "after_id is not a valid id"}
	}
	return id, nil
}
