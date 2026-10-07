package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store/db"
)

// NewItem is what InsertItem stores.
type NewItem struct {
	ID      uuid.UUID
	OwnerID uuid.UUID
	Input   domain.ItemInput
	Now     time.Time
}

// InsertItem stores a planned item.
func (r *Repo) InsertItem(ctx context.Context, n NewItem) (db.Item, error) {
	row, err := r.q.InsertItem(ctx, db.InsertItemParams{
		ID: n.ID, OwnerID: n.OwnerID, Title: n.Input.Title, Kind: string(n.Input.Kind),
		Url: textPtr(n.Input.URL), Insight: textPtr(n.Input.Insight), Now: n.Now,
	})
	return row, wrap("insert item", err)
}

// GetItem returns one of the owner's items, or ErrNotFound.
func (r *Repo) GetItem(ctx context.Context, owner, id uuid.UUID) (db.Item, error) {
	row, err := r.q.GetItem(ctx, db.GetItemParams{OwnerID: owner, ID: id})
	return row, wrap("get item", err)
}

// ListItems returns one page of the owner's items, newest first, and the token
// for the next page (empty on the last page). A nil status lists every status.
func (r *Repo) ListItems(ctx context.Context, owner uuid.UUID, status *domain.ItemStatus, page Page) ([]db.Item, string, error) {
	c, err := decodeCursor(page.Token)
	if err != nil {
		return nil, "", err
	}
	size := page.size()
	afterAt, afterID := afterParams(c)
	var statusText *string
	if status != nil {
		s := string(*status)
		statusText = &s
	}
	rows, err := r.q.ListItems(ctx, db.ListItemsParams{
		OwnerID: owner, Status: statusText, AfterCreatedAt: afterAt, AfterID: afterID,
		RowLimit: size + 1, // one look-ahead row tells whether another page exists
	})
	if err != nil {
		return nil, "", wrap("list items", err)
	}
	rows, next := trimPage(rows, size, func(i db.Item) (time.Time, uuid.UUID) { return i.CreatedAt, i.ID })
	return rows, next, nil
}

// UpdateItem writes the editable columns of the item the caller read at
// arg.Version. It returns ErrVersionConflict when the version is stale and
// ErrNotFound when the item does not exist.
func (r *Repo) UpdateItem(ctx context.Context, arg db.UpdateItemParams) (db.Item, error) {
	row, err := r.q.UpdateItem(ctx, arg)
	if err != nil {
		return db.Item{}, r.staleOrMissing("update item", err, arg.OwnerID, arg.ID)
	}
	return row, nil
}

// UpdateItemStatus writes a status and its dates under the same version check
// as UpdateItem.
func (r *Repo) UpdateItemStatus(ctx context.Context, arg db.UpdateItemStatusParams) (db.Item, error) {
	row, err := r.q.UpdateItemStatus(ctx, arg)
	if err != nil {
		return db.Item{}, r.staleOrMissing("update item status", err, arg.OwnerID, arg.ID)
	}
	return row, nil
}

// staleOrMissing tells a failed versioned update apart: the row is there but
// its version moved, or it is not there at all.
func (r *Repo) staleOrMissing(op string, err error, owner, id uuid.UUID) error {
	if !errors.Is(mapErr(err), ErrNotFound) {
		return wrap(op, err)
	}
	if _, getErr := r.q.GetItem(context.Background(), db.GetItemParams{OwnerID: owner, ID: id}); getErr != nil {
		return wrap(op, getErr)
	}
	return ErrVersionConflict
}

// trimPage cuts the look-ahead row off a page and returns the token for the
// next page when there was one.
func trimPage[T any](rows []T, size int32, key func(T) (time.Time, uuid.UUID)) ([]T, string) {
	if int32(len(rows)) <= size {
		return rows, ""
	}
	rows = rows[:size]
	at, id := key(rows[size-1])
	return rows, cursor{CreatedAt: at, ID: id}.encode()
}

// textPtr stores empty text as NULL.
func textPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
