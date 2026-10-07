package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store/db"
)

// ActivityPage is one page of activities.
type ActivityPage struct {
	Activities    []db.Activity
	NextPageToken string
}

// ActivityDetail is an activity with its item, when it has one.
type ActivityDetail struct {
	Activity db.Activity
	Item     *db.Item
}

// LogActivity records what the owner learned, optionally against one of their
// items, and emits learning.activity_added in the same transaction.
func (s *Service) LogActivity(ctx context.Context, itemID *uuid.UUID, in domain.ActivityInput) (db.Activity, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Activity{}, err
	}
	clean, err := in.Validate(s.today())
	if err != nil {
		return db.Activity{}, err
	}
	var out db.Activity
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		repo := store.New(tx)
		if itemID != nil {
			// The foreign key cannot tell whose item it is, so check here.
			if _, err := repo.GetItem(ctx, owner, *itemID); err != nil {
				return notFound(err, ErrItemNotFound)
			}
		}
		out, err = repo.InsertActivity(ctx, store.NewActivity{
			ID: store.NewID(), OwnerID: owner, ItemID: itemID, Input: clean, CreatedAt: s.now().UTC(),
		})
		if err != nil {
			return err
		}
		_, err = outbox.Write(ctx, tx, eventSource, eventActivityAdded, out.ID.String(), &dojov1.LearningActivityAdded{
			ActivityId: out.ID.String(), ItemId: idString(out.ItemID), Summary: out.Summary, OwnerId: owner.String(),
		})
		return wrapOp("write "+eventActivityAdded+" event", err)
	})
	return out, wrapOp("log activity", err)
}

// GetActivity returns one of the owner's activities with its item.
func (s *Service) GetActivity(ctx context.Context, id uuid.UUID) (ActivityDetail, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ActivityDetail{}, err
	}
	repo := store.New(s.pool)
	activity, err := repo.GetActivity(ctx, owner, id)
	if err != nil {
		return ActivityDetail{}, wrapOp("get activity", notFound(err, ErrActivityNotFound))
	}
	detail := ActivityDetail{Activity: activity}
	if activity.ItemID == nil {
		return detail, nil
	}
	item, err := repo.GetItem(ctx, owner, *activity.ItemID)
	switch {
	case err == nil:
		detail.Item = &item
	case !isNotFound(err): // an archived item leaves the activity without one
		return ActivityDetail{}, wrapOp("get activity item", err)
	}
	return detail, nil
}

// ListActivities returns one page of the owner's activities, newest first. A
// nil itemID lists every activity.
func (s *Service) ListActivities(ctx context.Context, itemID *uuid.UUID, page store.Page) (ActivityPage, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ActivityPage{}, err
	}
	rows, next, err := store.New(s.pool).ListActivities(ctx, owner, itemID, page)
	if err != nil {
		return ActivityPage{}, wrapOp("list activities", err)
	}
	return ActivityPage{Activities: rows, NextPageToken: next}, nil
}

func idString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
