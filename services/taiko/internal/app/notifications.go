package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

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

// parseIDs reads UUID arguments, refusing the whole call when one is malformed.
func parseIDs(field string, values []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(values))
	for _, v := range values {
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, &InvalidArgumentError{Reason: "INVALID_ID", Msg: field + " holds an id that is not valid"}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Listing is one page of notifications and the owner's total unread count.
type Listing struct {
	Notifications []db.Notification
	NextPageToken string
	UnreadCount   int32
}

// List returns one page of the owner's notifications, newest first.
func (s *Service) List(ctx context.Context, unreadOnly bool, page store.Page) (Listing, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return Listing{}, err
	}
	repo := store.New(s.pool)
	rows, next, err := repo.List(ctx, owner, unreadOnly, page)
	if err != nil {
		return Listing{}, err
	}
	unread, err := repo.CountUnread(ctx, owner)
	if err != nil {
		return Listing{}, err
	}
	return Listing{Notifications: rows, NextPageToken: next, UnreadCount: unread}, nil
}

// MarkRead marks the listed notifications read. Unknown ids are ignored.
func (s *Service) MarkRead(ctx context.Context, ids []string) error {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return err
	}
	parsed, err := parseIDs("ids", ids)
	if err != nil {
		return err
	}
	if len(parsed) == 0 {
		return nil
	}
	_, err = store.New(s.pool).MarkRead(ctx, owner, parsed, s.now().UTC())
	return err
}

// MarkAllRead marks every unread notification of the owner read.
func (s *Service) MarkAllRead(ctx context.Context) error {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return err
	}
	_, err = store.New(s.pool).MarkAllRead(ctx, owner, s.now().UTC())
	return err
}
