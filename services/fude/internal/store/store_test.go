package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
	"github.com/0xHoaxen/shogun/services/fude/migrations"
)

func newRepo(t *testing.T) (*store.Repo, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "fude")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.New(pool), pool
}

func newDraft(owner uuid.UUID, key *string) db.InsertDraftParams {
	return db.InsertDraftParams{
		ID: store.NewID(), OwnerID: owner, Kind: "cover_letter", TargetType: "job",
		Channel: "email", IdempotencyKey: key,
	}
}

func TestInsertAndGetDraft(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := store.NewID()

	d, err := repo.InsertDraft(ctx, newDraft(owner, nil))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := repo.GetDraft(ctx, owner, d.ID)

	if err != nil || got.State != "generating" || got.Version != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := repo.GetDraft(ctx, store.NewID(), d.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("another owner should not see the draft, got %v", err)
	}
}

func TestInsertDraftRepeatedKeyIsDuplicate(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := store.NewID()
	key := "k1"
	first, err := repo.InsertDraft(ctx, newDraft(owner, &key))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, err = repo.InsertDraft(ctx, newDraft(owner, &key))
	replay, getErr := repo.GetDraftByIdempotencyKey(ctx, owner, key)

	if !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
	if getErr != nil || replay.ID != first.ID {
		t.Fatalf("replay %v, %v", replay.ID, getErr)
	}
}

func TestUpdateDraftStateChecksVersion(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := store.NewID()
	d, _ := repo.InsertDraft(ctx, newDraft(owner, nil))
	arg := db.UpdateDraftStateParams{ID: d.ID, OwnerID: owner, State: "pending", CurrentVersion: 1, Version: d.Version}

	updated, err := repo.UpdateDraftState(ctx, arg)
	_, staleErr := repo.UpdateDraftState(ctx, arg)
	_, missingErr := repo.UpdateDraftState(ctx, db.UpdateDraftStateParams{ID: store.NewID(), OwnerID: owner, Version: 1})

	if err != nil || updated.State != "pending" || updated.Version != 2 || updated.CurrentVersion != 1 {
		t.Fatalf("got %+v, %v", updated, err)
	}
	if !errors.Is(staleErr, store.ErrVersionConflict) {
		t.Fatalf("stale version: want ErrVersionConflict, got %v", staleErr)
	}
	if !errors.Is(missingErr, store.ErrNotFound) {
		t.Fatalf("missing draft: want ErrNotFound, got %v", missingErr)
	}
}

func TestListDraftsPaginatesByStateNewestFirst(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	owner := store.NewID()
	var ids []uuid.UUID
	for i := range 5 {
		d, _ := repo.InsertDraft(ctx, newDraft(owner, nil))
		ids = append(ids, d.ID)
		if _, err := pool.Exec(ctx, `UPDATE drafts SET state = 'pending', updated_at = $2 WHERE id = $1`,
			d.ID, time.Date(2026, 10, 1, 0, i, 0, 0, time.UTC)); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if _, err := repo.InsertDraft(ctx, newDraft(owner, nil)); err != nil { // stays generating
		t.Fatal(err)
	}

	page1, next, err := repo.ListDrafts(ctx, owner, "pending", store.Page{Size: 2})
	if err != nil || len(page1) != 2 || next == "" {
		t.Fatalf("page1 %d next %q err %v", len(page1), next, err)
	}
	page2, next2, _ := repo.ListDrafts(ctx, owner, "pending", store.Page{Size: 2, Token: next})
	page3, next3, _ := repo.ListDrafts(ctx, owner, "pending", store.Page{Size: 2, Token: next2})

	if page1[0].ID != ids[4] || page2[0].ID != ids[2] || len(page3) != 1 || page3[0].ID != ids[0] || next3 != "" {
		t.Fatalf("unexpected order: %v %v %v next3=%q", page1, page2, page3, next3)
	}
	if _, _, err := repo.ListDrafts(ctx, owner, "pending", store.Page{Token: "!!"}); !errors.Is(err, store.ErrInvalidPageToken) {
		t.Fatalf("want ErrInvalidPageToken, got %v", err)
	}
}

func TestDraftVersionsNewestFirst(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	d, _ := repo.InsertDraft(ctx, newDraft(store.NewID(), nil))
	for v := int32(1); v <= 2; v++ {
		if _, err := repo.InsertDraftVersion(ctx, db.InsertDraftVersionParams{
			DraftID: d.ID, Version: v, Body: "b", BodySha256: []byte{byte(v)}, CreatedBy: "ai",
		}); err != nil {
			t.Fatalf("insert v%d: %v", v, err)
		}
	}
	_, dupErr := repo.InsertDraftVersion(ctx, db.InsertDraftVersionParams{
		DraftID: d.ID, Version: 2, Body: "b", BodySha256: []byte{9}, CreatedBy: "ai",
	})

	vs, err := repo.ListDraftVersions(ctx, d.ID)

	if err != nil || len(vs) != 2 || vs[0].Version != 2 {
		t.Fatalf("got %+v, %v", vs, err)
	}
	if !errors.Is(dupErr, store.ErrDuplicate) {
		t.Fatalf("repeated version: want ErrDuplicate, got %v", dupErr)
	}
}

func TestInsertVoiceSample(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	s, err := repo.InsertVoiceSample(ctx, db.InsertVoiceSampleParams{ID: store.NewID(), OwnerID: store.NewID(), Channel: "email", Text: "hi"})

	var nilEmbedding bool
	_ = pool.QueryRow(ctx, `SELECT embedding IS NULL FROM voice_samples WHERE id = $1`, s.ID).Scan(&nilEmbedding)
	if err != nil || !nilEmbedding {
		t.Fatalf("got %+v, nil embedding %v, err %v", s, nilEmbedding, err)
	}
}
