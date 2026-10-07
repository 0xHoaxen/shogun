package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store/db"
)

// NewActivity is what InsertActivity stores.
type NewActivity struct {
	ID      uuid.UUID
	OwnerID uuid.UUID
	// ItemID is the owner's item the activity belongs to; nil for none. The
	// caller checks the item is the owner's, since the foreign key cannot.
	ItemID    *uuid.UUID
	Input     domain.ActivityInput
	CreatedAt time.Time
}

// InsertActivity stores an activity.
func (r *Repo) InsertActivity(ctx context.Context, n NewActivity) (db.Activity, error) {
	var minutes *int32
	if n.Input.Minutes > 0 {
		minutes = &n.Input.Minutes
	}
	tags := n.Input.Tags
	if tags == nil {
		tags = []string{} // tags is NOT NULL; a nil slice would be sent as NULL
	}
	row, err := r.q.InsertActivity(ctx, db.InsertActivityParams{
		ID: n.ID, OwnerID: n.OwnerID, ItemID: n.ItemID, Summary: n.Input.Summary, Minutes: minutes,
		OccurredOn: n.Input.OccurredOn, Tags: tags, CreatedAt: n.CreatedAt,
	})
	return row, wrap("insert activity", err)
}

// GetActivity returns one of the owner's activities, or ErrNotFound.
func (r *Repo) GetActivity(ctx context.Context, owner, id uuid.UUID) (db.Activity, error) {
	row, err := r.q.GetActivity(ctx, db.GetActivityParams{OwnerID: owner, ID: id})
	return row, wrap("get activity", err)
}

// ListActivities returns one page of the owner's activities, newest first, and
// the token for the next page (empty on the last page). A nil itemID lists
// every activity.
func (r *Repo) ListActivities(ctx context.Context, owner uuid.UUID, itemID *uuid.UUID, page Page) ([]db.Activity, string, error) {
	c, err := decodeCursor(page.Token)
	if err != nil {
		return nil, "", err
	}
	size := page.size()
	afterAt, afterID := afterParams(c)
	rows, err := r.q.ListActivities(ctx, db.ListActivitiesParams{
		OwnerID: owner, ItemID: itemID, AfterCreatedAt: afterAt, AfterID: afterID, RowLimit: size + 1,
	})
	if err != nil {
		return nil, "", wrap("list activities", err)
	}
	rows, next := trimPage(rows, size, func(a db.Activity) (time.Time, uuid.UUID) { return a.CreatedAt, a.ID })
	return rows, next, nil
}
