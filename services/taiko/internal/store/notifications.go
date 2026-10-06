package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/taiko/internal/domain"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

// NewNotification is what Insert stores.
type NewNotification struct {
	ID        uuid.UUID
	OwnerID   uuid.UUID
	Notice    domain.Notice
	CreatedAt time.Time
	// SourceEventID makes the insert idempotent: a second notification for the
	// same event is not stored. Nil for notifications that no event caused.
	SourceEventID *uuid.UUID
}

// Insert stores a notification. It reports false, and stores nothing, when one
// for the same source event already exists.
func (r *Repo) Insert(ctx context.Context, n NewNotification) (db.Notification, bool, error) {
	row, err := r.q.InsertNotification(ctx, db.InsertNotificationParams{
		ID: n.ID, OwnerID: n.OwnerID, Type: string(n.Notice.Type), Title: n.Notice.Title,
		Body: textPtr(n.Notice.Body), Link: textPtr(n.Notice.Link),
		SourceEventID: n.SourceEventID, CreatedAt: n.CreatedAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Notification{}, false, nil
	}
	if err != nil {
		return db.Notification{}, false, wrap("insert notification", err)
	}
	return row, true, nil
}

// List returns one page of the owner's notifications, newest first, and the
// token for the next page (empty on the last page).
func (r *Repo) List(ctx context.Context, owner uuid.UUID, unreadOnly bool, page Page) ([]db.Notification, string, error) {
	c, err := decodeCursor(page.Token)
	if err != nil {
		return nil, "", err
	}
	size := page.size()
	afterAt, afterID := afterParams(c)
	rows, err := r.q.ListNotifications(ctx, db.ListNotificationsParams{
		OwnerID: owner, UnreadOnly: unreadOnly, AfterCreatedAt: afterAt, AfterID: afterID,
		RowLimit: size + 1, // one look-ahead row tells whether another page exists
	})
	if err != nil {
		return nil, "", wrap("list notifications", err)
	}
	if int32(len(rows)) <= size {
		return rows, "", nil
	}
	rows = rows[:size]
	last := rows[size-1]
	return rows, cursor{CreatedAt: last.CreatedAt, ID: last.ID}.encode(), nil
}

// ListAfter returns up to limit notifications created after afterID, oldest
// first. A stream uses it to replay what a client missed.
func (r *Repo) ListAfter(ctx context.Context, owner, afterID uuid.UUID, limit int32) ([]db.Notification, error) {
	rows, err := r.q.ListNotificationsAfter(ctx, db.ListNotificationsAfterParams{
		OwnerID: owner, AfterID: afterID, RowLimit: limit,
	})
	return rows, wrap("list notifications after", err)
}

// CountUnread returns how many notifications the owner has not read.
func (r *Repo) CountUnread(ctx context.Context, owner uuid.UUID) (int32, error) {
	n, err := r.q.CountUnread(ctx, owner)
	return n, wrap("count unread", err)
}

// MarkRead marks the owner's listed notifications read at at, and returns how
// many changed. Ids that are unknown, someone else's or already read count as
// zero.
func (r *Repo) MarkRead(ctx context.Context, owner uuid.UUID, ids []uuid.UUID, at time.Time) (int64, error) {
	n, err := r.q.MarkNotificationsRead(ctx, db.MarkNotificationsReadParams{
		OwnerID: owner, Ids: ids, ReadAt: &at,
	})
	return n, wrap("mark read", err)
}

// MarkAllRead marks every unread notification of the owner read at at.
func (r *Repo) MarkAllRead(ctx context.Context, owner uuid.UUID, at time.Time) (int64, error) {
	n, err := r.q.MarkAllNotificationsRead(ctx, db.MarkAllNotificationsReadParams{OwnerID: owner, ReadAt: &at})
	return n, wrap("mark all read", err)
}

// textPtr stores empty text as NULL.
func textPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
