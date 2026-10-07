package store_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store/db"
	"github.com/0xHoaxen/shogun/services/dojo/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

var base = time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)

func newRepo(t *testing.T) (*store.Repo, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "dojo")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.New(pool), pool
}

// addItem stores an item created i minutes after base.
func addItem(t *testing.T, repo *store.Repo, owner uuid.UUID, title string, i int) db.Item {
	t.Helper()
	row, err := repo.InsertItem(context.Background(), store.NewItem{
		ID: store.NewID(), OwnerID: owner, Now: base.Add(time.Duration(i) * time.Minute),
		Input: domain.ItemInput{Title: title, Kind: domain.KindCourse, URL: "https://example.com/" + title, Insight: "notes"},
	})
	if err != nil {
		t.Fatalf("insert item %q: %v", title, err)
	}
	return row
}

// addActivity stores an activity created i minutes after base.
func addActivity(t *testing.T, repo *store.Repo, owner uuid.UUID, item *uuid.UUID, summary string, i int) db.Activity {
	t.Helper()
	row, err := repo.InsertActivity(context.Background(), store.NewActivity{
		ID: store.NewID(), OwnerID: owner, ItemID: item, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		Input: domain.ActivityInput{Summary: summary, Minutes: 30, OccurredOn: base, Tags: []string{"go"}},
	})
	if err != nil {
		t.Fatalf("insert activity %q: %v", summary, err)
	}
	return row
}

func TestInsertItemStartsPlannedAtVersionOne(t *testing.T) {
	repo, _ := newRepo(t)
	owner := uuid.New()

	row := addItem(t, repo, owner, "go", 0)

	if row.Status != "planned" || row.Version != 1 || row.StartedOn != nil || row.CompletedOn != nil {
		t.Fatalf("row = %+v, want planned, version 1, no dates", row)
	}
	if row.Url == nil || *row.Url != "https://example.com/go" {
		t.Fatalf("url = %v", row.Url)
	}
}

func TestGetItemIsPerOwner(t *testing.T) {
	repo, _ := newRepo(t)
	owner := uuid.New()
	row := addItem(t, repo, owner, "go", 0)

	got, err := repo.GetItem(context.Background(), owner, row.ID)
	_, otherErr := repo.GetItem(context.Background(), uuid.New(), row.ID)

	if err != nil || got.ID != row.ID {
		t.Fatalf("get own item: %v, %v", got.ID, err)
	}
	if !errors.Is(otherErr, store.ErrNotFound) {
		t.Fatalf("another owner's get = %v, want ErrNotFound", otherErr)
	}
}

func TestUpdateItemChecksTheVersion(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	row := addItem(t, repo, owner, "go", 0)
	update := func(version int32, id uuid.UUID) (db.Item, error) {
		return repo.UpdateItem(ctx, db.UpdateItemParams{
			ID: id, OwnerID: owner, Version: version, Title: "go 2", Kind: "book", Now: base.Add(time.Hour),
		})
	}

	updated, err := update(1, row.ID)
	_, stale := update(1, row.ID)
	_, missing := update(1, uuid.New())

	if err != nil || updated.Version != 2 || updated.Title != "go 2" || updated.Kind != "book" {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if updated.Url != nil || updated.Insight != nil {
		t.Fatalf("url %v and insight %v, want both cleared by the update", updated.Url, updated.Insight)
	}
	if !errors.Is(stale, store.ErrVersionConflict) {
		t.Fatalf("stale update = %v, want ErrVersionConflict", stale)
	}
	if !errors.Is(missing, store.ErrNotFound) {
		t.Fatalf("missing update = %v, want ErrNotFound", missing)
	}
}

func TestUpdateItemStatusStoresTheDatesAndChecksTheVersion(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	row := addItem(t, repo, owner, "go", 0)
	done := base.AddDate(0, 0, 1)
	arg := db.UpdateItemStatusParams{
		ID: row.ID, OwnerID: owner, Version: 1, Status: "done", StartedOn: &base, CompletedOn: &done, Now: done,
	}

	got, err := repo.UpdateItemStatus(ctx, arg)
	_, stale := repo.UpdateItemStatus(ctx, arg)

	if err != nil || got.Status != "done" || got.Version != 2 {
		t.Fatalf("status update = %+v, %v", got, err)
	}
	if got.StartedOn == nil || got.CompletedOn == nil || got.CompletedOn.Sub(*got.StartedOn) != 24*time.Hour {
		t.Fatalf("dates = %v, %v, want one day apart", got.StartedOn, got.CompletedOn)
	}
	if !errors.Is(stale, store.ErrVersionConflict) {
		t.Fatalf("stale status update = %v, want ErrVersionConflict", stale)
	}
}

func TestListItemsPagesNewestFirstAndFiltersByStatus(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	var ids []uuid.UUID
	for i, title := range []string{"a", "b", "c", "d", "e"} {
		ids = append(ids, addItem(t, repo, owner, title, i).ID)
	}
	addItem(t, repo, uuid.New(), "someone else's", 1)
	if _, err := repo.UpdateItemStatus(ctx, db.UpdateItemStatusParams{
		ID: ids[0], OwnerID: owner, Version: 1, Status: "in_progress", StartedOn: &base, Now: base,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	var got []uuid.UUID
	token := ""
	pages := 0
	for {
		rows, next, err := repo.ListItems(ctx, owner, nil, store.Page{Size: 2, Token: token})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, r := range rows {
			got = append(got, r.ID)
		}
		pages++
		if next == "" {
			break
		}
		token = next
	}
	inProgress := domain.StatusInProgress
	filtered, _, err := repo.ListItems(ctx, owner, &inProgress, store.Page{})

	slices.Reverse(ids)
	if !slices.Equal(got, ids) || pages != 3 {
		t.Fatalf("paged ids = %v over %d pages, want %v over 3", got, pages, ids)
	}
	if err != nil || len(filtered) != 1 || filtered[0].ID != ids[4] {
		t.Fatalf("filtered = %v, %v", filtered, err)
	}
}

func TestListRejectsAGarbagePageToken(t *testing.T) {
	repo, _ := newRepo(t)

	_, _, itemsErr := repo.ListItems(context.Background(), uuid.New(), nil, store.Page{Token: "!!"})
	_, _, activitiesErr := repo.ListActivities(context.Background(), uuid.New(), nil, store.Page{Token: "!!"})

	if !errors.Is(itemsErr, store.ErrInvalidPageToken) || !errors.Is(activitiesErr, store.ErrInvalidPageToken) {
		t.Fatalf("errors = %v, %v, want ErrInvalidPageToken", itemsErr, activitiesErr)
	}
}

func TestActivitiesStoreListAndFilterByItem(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	item := addItem(t, repo, owner, "go", 0)
	first := addActivity(t, repo, owner, &item.ID, "chapter 1", 1)
	loose := addActivity(t, repo, owner, nil, "podcast", 2)
	last := addActivity(t, repo, owner, &item.ID, "chapter 2", 3)
	addActivity(t, repo, uuid.New(), nil, "someone else's", 4)

	all, next, err := repo.ListActivities(ctx, owner, nil, store.Page{})
	forItem, _, itemErr := repo.ListActivities(ctx, owner, &item.ID, store.Page{})
	firstPage, token, pageErr := repo.ListActivities(ctx, owner, nil, store.Page{Size: 2})
	secondPage, end, secondErr := repo.ListActivities(ctx, owner, nil, store.Page{Size: 2, Token: token})

	if err != nil || next != "" || !slices.Equal(ids(all), []uuid.UUID{last.ID, loose.ID, first.ID}) {
		t.Fatalf("all = %v, next %q, %v", ids(all), next, err)
	}
	if itemErr != nil || !slices.Equal(ids(forItem), []uuid.UUID{last.ID, first.ID}) {
		t.Fatalf("for item = %v, %v", ids(forItem), itemErr)
	}
	if pageErr != nil || secondErr != nil || token == "" || end != "" ||
		!slices.Equal(ids(firstPage), []uuid.UUID{last.ID, loose.ID}) || !slices.Equal(ids(secondPage), []uuid.UUID{first.ID}) {
		t.Fatalf("pages = %v then %v (token %q, end %q), %v %v", ids(firstPage), ids(secondPage), token, end, pageErr, secondErr)
	}
	if first.Minutes == nil || *first.Minutes != 30 || !slices.Equal(first.Tags, []string{"go"}) {
		t.Fatalf("minutes %v tags %v", first.Minutes, first.Tags)
	}
}

func TestActivityWithoutMinutesStoresNull(t *testing.T) {
	repo, _ := newRepo(t)

	row, err := repo.InsertActivity(context.Background(), store.NewActivity{
		ID: store.NewID(), OwnerID: uuid.New(), CreatedAt: base,
		Input: domain.ActivityInput{Summary: "x", OccurredOn: base},
	})

	if err != nil || row.Minutes != nil || row.Tags == nil {
		t.Fatalf("row = %+v, %v; want NULL minutes and an empty, non-nil tag list", row, err)
	}
}

func TestGetActivityIsPerOwner(t *testing.T) {
	repo, _ := newRepo(t)
	owner := uuid.New()
	row := addActivity(t, repo, owner, nil, "x", 0)

	got, err := repo.GetActivity(context.Background(), owner, row.ID)
	_, otherErr := repo.GetActivity(context.Background(), uuid.New(), row.ID)

	if err != nil || got.ID != row.ID || !errors.Is(otherErr, store.ErrNotFound) {
		t.Fatalf("own = %v, %v; other = %v", got.ID, err, otherErr)
	}
}

func TestMigrationsEnforceTheChecks(t *testing.T) {
	_, pool := newRepo(t)
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"known kind", `INSERT INTO items (id, owner_id, title, kind) VALUES (gen_random_uuid(), gen_random_uuid(), 'x', 'book')`, false},
		{"unknown kind", `INSERT INTO items (id, owner_id, title, kind) VALUES (gen_random_uuid(), gen_random_uuid(), 'x', 'podcast')`, true},
		{"unknown status", `INSERT INTO items (id, owner_id, title, kind, status) VALUES (gen_random_uuid(), gen_random_uuid(), 'x', 'book', 'paused')`, true},
		{"activity of a missing item", `INSERT INTO activities (id, owner_id, item_id, summary) VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'x')`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(context.Background(), tt.query)

			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func ids(rows []db.Activity) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}
