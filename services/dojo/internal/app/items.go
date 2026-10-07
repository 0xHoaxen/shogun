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
	"github.com/0xHoaxen/shogun/services/dojo/internal/wire"
)

// ItemPage is one page of items.
type ItemPage struct {
	Items         []db.Item
	NextPageToken string
}

// ItemPatch holds the item fields an update changes; a nil field is left alone.
type ItemPatch struct {
	Title   *string
	Kind    *domain.ItemKind
	URL     *string
	Insight *string
}

// AddItem stores a planned item for the owner.
func (s *Service) AddItem(ctx context.Context, in domain.ItemInput) (db.Item, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Item{}, err
	}
	clean, err := in.Validate()
	if err != nil {
		return db.Item{}, err
	}
	row, err := store.New(s.pool).InsertItem(ctx, store.NewItem{
		ID: store.NewID(), OwnerID: owner, Input: clean, Now: s.now().UTC(),
	})
	return row, wrapOp("add item", err)
}

// GetItem returns one of the owner's items.
func (s *Service) GetItem(ctx context.Context, id uuid.UUID) (db.Item, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Item{}, err
	}
	row, err := store.New(s.pool).GetItem(ctx, owner, id)
	return row, wrapOp("get item", notFound(err, ErrItemNotFound))
}

// ListItems returns one page of the owner's items, newest first. A nil status
// lists every status.
func (s *Service) ListItems(ctx context.Context, status *domain.ItemStatus, page store.Page) (ItemPage, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ItemPage{}, err
	}
	rows, next, err := store.New(s.pool).ListItems(ctx, owner, status, page)
	if err != nil {
		return ItemPage{}, wrapOp("list items", err)
	}
	return ItemPage{Items: rows, NextPageToken: next}, nil
}

// UpdateItem applies patch to the item the caller read at version. A stale
// version fails with store.ErrVersionConflict.
func (s *Service) UpdateItem(ctx context.Context, id uuid.UUID, patch ItemPatch, version int32) (db.Item, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Item{}, err
	}
	repo := store.New(s.pool)
	current, err := repo.GetItem(ctx, owner, id)
	if err != nil {
		return db.Item{}, wrapOp("update item", notFound(err, ErrItemNotFound))
	}
	if current.Version != version {
		return db.Item{}, store.ErrVersionConflict
	}
	in, err := applyPatch(current, patch).Validate()
	if err != nil {
		return db.Item{}, err
	}
	row, err := repo.UpdateItem(ctx, db.UpdateItemParams{
		ID: id, OwnerID: owner, Version: version, Title: in.Title, Kind: string(in.Kind),
		Url: textPtr(in.URL), Insight: textPtr(in.Insight), Now: s.now().UTC(),
	})
	return row, wrapOp("update item", notFound(err, ErrItemNotFound))
}

// ChangeItemStatus moves the item the caller read at version to next, and
// emits learning.item_completed in the same transaction when it is now done.
func (s *Service) ChangeItemStatus(ctx context.Context, id uuid.UUID, next domain.ItemStatus, version int32) (db.Item, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Item{}, err
	}
	if !next.Valid() {
		return db.Item{}, invalidInput("status is not known")
	}
	var out db.Item
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		repo := store.New(tx)
		current, err := repo.GetItem(ctx, owner, id)
		if err != nil {
			return notFound(err, ErrItemNotFound)
		}
		if current.Version != version {
			return store.ErrVersionConflict
		}
		moved, err := domain.Item{
			Status: domain.ItemStatus(current.Status), StartedOn: current.StartedOn, CompletedOn: current.CompletedOn,
		}.ChangeStatus(next, s.today())
		if err != nil {
			return err
		}
		out, err = repo.UpdateItemStatus(ctx, db.UpdateItemStatusParams{
			ID: id, OwnerID: owner, Version: version, Status: string(moved.Status),
			StartedOn: moved.StartedOn, CompletedOn: moved.CompletedOn, Now: s.now().UTC(),
		})
		if err != nil {
			return notFound(err, ErrItemNotFound)
		}
		if next != domain.StatusDone {
			return nil
		}
		_, err = outbox.Write(ctx, tx, eventSource, eventItemCompleted, out.ID.String(), &dojov1.LearningItemCompleted{
			ItemId: out.ID.String(), Title: out.Title, Kind: wire.KindToProto(domain.ItemKind(out.Kind)), OwnerId: owner.String(),
		})
		return wrapOp("write "+eventItemCompleted+" event", err)
	})
	return out, wrapOp("change item status", err)
}

// applyPatch returns the item's editable fields with patch laid over them.
func applyPatch(row db.Item, p ItemPatch) domain.ItemInput {
	in := domain.ItemInput{
		Title: row.Title, Kind: domain.ItemKind(row.Kind), URL: deref(row.Url), Insight: deref(row.Insight),
	}
	if p.Title != nil {
		in.Title = *p.Title
	}
	if p.Kind != nil {
		in.Kind = *p.Kind
	}
	if p.URL != nil {
		in.URL = *p.URL
	}
	if p.Insight != nil {
		in.Insight = *p.Insight
	}
	return in
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// textPtr stores empty text as NULL.
func textPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
