package store_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/taiko/internal/domain"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
	"github.com/0xHoaxen/shogun/services/taiko/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

var base = time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)

func newRepo(t *testing.T) (*store.Repo, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "taiko")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.New(pool), pool
}

func notice(t *testing.T, title string) domain.Notice {
	t.Helper()
	n, err := domain.NewNotice(domain.TypeDraftReady, title, "body", "/drafts/1")
	if err != nil {
		t.Fatalf("notice: %v", err)
	}
	return n
}

// add stores a notification created i minutes after base.
func add(t *testing.T, repo *store.Repo, owner uuid.UUID, title string, i int) db.Notification {
	t.Helper()
	row, created, err := repo.Insert(context.Background(), store.NewNotification{
		ID: store.NewID(), OwnerID: owner, Notice: notice(t, title), CreatedAt: base.Add(time.Duration(i) * time.Minute),
	})
	if err != nil || !created {
		t.Fatalf("insert %q: created=%v, %v", title, created, err)
	}
	return row
}

func TestInsertStoresTheNotification(t *testing.T) {
	repo, _ := newRepo(t)
	owner := store.NewID()
	event := store.NewID()

	row, created, err := repo.Insert(context.Background(), store.NewNotification{
		ID: store.NewID(), OwnerID: owner, Notice: notice(t, "Ready"), CreatedAt: base, SourceEventID: &event,
	})

	if err != nil || !created {
		t.Fatalf("created=%v, %v", created, err)
	}
	if row.Title != "Ready" || row.Type != "draft_ready" || row.ReadAt != nil || !row.CreatedAt.Equal(base) {
		t.Fatalf("got %+v", row)
	}
	if row.Body == nil || *row.Body != "body" || row.Link == nil || *row.Link != "/drafts/1" {
		t.Fatalf("body and link not stored: %+v", row)
	}
}

func TestInsertStoresEmptyBodyAndLinkAsNull(t *testing.T) {
	repo, _ := newRepo(t)
	n, _ := domain.NewNotice(domain.TypeOffer, "Offer", "", "")

	row, _, err := repo.Insert(context.Background(), store.NewNotification{
		ID: store.NewID(), OwnerID: store.NewID(), Notice: n, CreatedAt: base,
	})

	if err != nil || row.Body != nil || row.Link != nil {
		t.Fatalf("got %+v, %v", row, err)
	}
}

func TestInsertSameSourceEventStoresOnce(t *testing.T) {
	repo, pool := newRepo(t)
	owner, event := store.NewID(), store.NewID()
	in := func() store.NewNotification {
		return store.NewNotification{ID: store.NewID(), OwnerID: owner, Notice: notice(t, "Ready"), CreatedAt: base, SourceEventID: &event}
	}

	_, first, err1 := repo.Insert(context.Background(), in())
	_, second, err2 := repo.Insert(context.Background(), in())

	if err1 != nil || err2 != nil || !first || second {
		t.Fatalf("first=%v (%v), second=%v (%v)", first, err1, second, err2)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM notifications`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows %d, %v", n, err)
	}
}

func TestInsertWithoutSourceEventNeverConflicts(t *testing.T) {
	repo, _ := newRepo(t)
	owner := store.NewID()

	add(t, repo, owner, "a", 0)
	add(t, repo, owner, "b", 1)
}

func TestListPagesNewestFirst(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := store.NewID()
	for i, title := range []string{"first", "second", "third", "fourth", "fifth"} {
		add(t, repo, owner, title, i)
	}
	add(t, repo, store.NewID(), "someone else", 9)

	page1, next1, err := repo.List(ctx, owner, false, store.Page{Size: 2})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	page2, next2, err := repo.List(ctx, owner, false, store.Page{Size: 2, Token: next1})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	page3, next3, err := repo.List(ctx, owner, false, store.Page{Size: 2, Token: next2})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}

	got := titles(page1, page2, page3)
	want := []string{"fifth", "fourth", "third", "second", "first"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if next1 == "" || next2 == "" || next3 != "" {
		t.Fatalf("tokens %q %q %q: only the last page should have none", next1, next2, next3)
	}
}

func TestListUnreadOnly(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := store.NewID()
	read := add(t, repo, owner, "read", 0)
	add(t, repo, owner, "unread", 1)
	if _, err := repo.MarkRead(ctx, owner, []uuid.UUID{read.ID}, base); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	unread, _, err := repo.List(ctx, owner, true, store.Page{})
	all, _, err2 := repo.List(ctx, owner, false, store.Page{})

	if err != nil || err2 != nil {
		t.Fatalf("list: %v, %v", err, err2)
	}
	if got := titles(unread); len(got) != 1 || got[0] != "unread" {
		t.Fatalf("unread only: %v", got)
	}
	if len(all) != 2 {
		t.Fatalf("all: %d", len(all))
	}
}

func TestListRejectsAMalformedToken(t *testing.T) {
	repo, _ := newRepo(t)

	_, _, err := repo.List(context.Background(), store.NewID(), false, store.Page{Token: "%%%"})

	if !errors.Is(err, store.ErrInvalidPageToken) {
		t.Fatalf("want ErrInvalidPageToken, got %v", err)
	}
}

func TestListAfterReplaysOldestFirst(t *testing.T) {
	repo, _ := newRepo(t)
	owner := store.NewID()
	first := add(t, repo, owner, "first", 0)
	add(t, repo, owner, "second", 1)
	add(t, repo, owner, "third", 2)
	add(t, repo, store.NewID(), "someone else", 3)

	rows, err := repo.ListAfter(context.Background(), owner, first.ID, 10)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if got := titles(rows); len(got) != 2 || got[0] != "second" || got[1] != "third" {
		t.Fatalf("got %v", got)
	}
}

func TestMarkReadOnlyTouchesTheOwnersUnreadRows(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner, other := store.NewID(), store.NewID()
	mine := add(t, repo, owner, "mine", 0)
	theirs := add(t, repo, other, "theirs", 0)

	changed, err := repo.MarkRead(ctx, owner, []uuid.UUID{mine.ID, theirs.ID, store.NewID()}, base)
	again, err2 := repo.MarkRead(ctx, owner, []uuid.UUID{mine.ID}, base.Add(time.Hour))

	if err != nil || err2 != nil {
		t.Fatalf("mark read: %v, %v", err, err2)
	}
	if changed != 1 || again != 0 {
		t.Fatalf("changed %d then %d, want 1 then 0", changed, again)
	}
	if n, _ := repo.CountUnread(ctx, other); n != 1 {
		t.Fatalf("another owner's unread count is %d, want 1", n)
	}
}

func TestMarkAllReadAndCountUnread(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := store.NewID()
	add(t, repo, owner, "a", 0)
	add(t, repo, owner, "b", 1)
	before, _ := repo.CountUnread(ctx, owner)

	changed, err := repo.MarkAllRead(ctx, owner, base)
	after, _ := repo.CountUnread(ctx, owner)

	if err != nil || before != 2 || changed != 2 || after != 0 {
		t.Fatalf("before %d, changed %d, after %d, err %v", before, changed, after, err)
	}
}

func TestChannelSettingDefaultsToNotFoundThenSaves(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := store.NewID()

	_, errBefore := repo.ChannelSetting(ctx, owner, store.ChannelInApp)
	saved, err := repo.SaveChannelSetting(ctx, db.UpsertChannelSettingParams{
		OwnerID: owner, Channel: store.ChannelInApp, Enabled: false,
	})
	replaced, err2 := repo.SaveChannelSetting(ctx, db.UpsertChannelSettingParams{
		OwnerID: owner, Channel: store.ChannelInApp, Enabled: true,
	})
	got, err3 := repo.ChannelSetting(ctx, owner, store.ChannelInApp)

	if !errors.Is(errBefore, store.ErrNotFound) {
		t.Fatalf("before saving want ErrNotFound, got %v", errBefore)
	}
	if err != nil || err2 != nil || err3 != nil || saved.Enabled || !replaced.Enabled || !got.Enabled {
		t.Fatalf("saved %+v (%v), replaced %+v (%v), got %+v (%v)", saved, err, replaced, err2, got, err3)
	}
}

func titles(pages ...[]db.Notification) []string {
	var out []string
	for _, rows := range pages {
		for _, r := range rows {
			out = append(out, r.Title)
		}
	}
	return out
}
