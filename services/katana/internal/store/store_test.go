package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
	"github.com/0xHoaxen/shogun/services/katana/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

var base = time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)

func newRepo(t *testing.T) (*store.Repo, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "katana")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.New(pool), pool
}

// add stores an open resume suggestion created i minutes after base.
func add(t *testing.T, repo *store.Repo, owner uuid.UUID, target domain.Target, i int) db.Suggestion {
	t.Helper()
	row, err := repo.InsertSuggestion(context.Background(), store.NewSuggestion{
		ID: store.NewID(), OwnerID: owner, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		Suggestion: domain.Suggestion{
			Target: target, Section: domain.SectionSkills, After: "Go, SQL", Reason: "used in 5 repos",
			Evidence: []domain.Evidence{{Label: "repo", URL: "https://github.com/o/r"}},
		},
	})
	if err != nil {
		t.Fatalf("insert suggestion: %v", err)
	}
	return row
}

func TestInsertSuggestionStoresItOpenWithItsEvidence(t *testing.T) {
	repo, _ := newRepo(t)

	row := add(t, repo, uuid.New(), domain.TargetResume, 0)

	var evidence []domain.Evidence
	if err := json.Unmarshal(row.Evidence, &evidence); err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if row.State != "open" || row.DecidedAt != nil || row.Before != nil || len(evidence) != 1 || evidence[0].Label != "repo" {
		t.Fatalf("row = %+v, evidence %v", row, evidence)
	}
}

func TestInsertSuggestionWithoutEvidenceStoresAnEmptyList(t *testing.T) {
	repo, _ := newRepo(t)

	row, err := repo.InsertSuggestion(context.Background(), store.NewSuggestion{
		ID: store.NewID(), OwnerID: uuid.New(), CreatedAt: base,
		Suggestion: domain.Suggestion{Target: domain.TargetLinkedIn, Section: domain.SectionAbout, After: "x", Reason: "y"},
	})

	if err != nil || string(row.Evidence) != "[]" {
		t.Fatalf("evidence = %s, err %v; want []", row.Evidence, err)
	}
}

func TestGetSuggestionIsPerOwner(t *testing.T) {
	repo, _ := newRepo(t)
	owner := uuid.New()
	row := add(t, repo, owner, domain.TargetResume, 0)

	_, ownErr := repo.GetSuggestion(context.Background(), owner, row.ID)
	_, otherErr := repo.GetSuggestion(context.Background(), uuid.New(), row.ID)

	if ownErr != nil || !errors.Is(otherErr, store.ErrNotFound) {
		t.Fatalf("own %v, other %v", ownErr, otherErr)
	}
}

func TestDecideMovesAnOpenSuggestionOnce(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	row := add(t, repo, owner, domain.TargetResume, 0)

	decided, err := repo.Decide(ctx, owner, row.ID, domain.StateAccepted, base.Add(time.Hour))
	_, again := repo.Decide(ctx, owner, row.ID, domain.StateDismissed, base.Add(2*time.Hour))
	_, missing := repo.Decide(ctx, owner, uuid.New(), domain.StateAccepted, base)
	_, foreign := repo.Decide(ctx, uuid.New(), row.ID, domain.StateAccepted, base)

	if err != nil || decided.State != "accepted" || decided.DecidedAt == nil || !decided.DecidedAt.Equal(base.Add(time.Hour)) {
		t.Fatalf("decided = %+v, %v", decided, err)
	}
	if !errors.Is(again, store.ErrNotOpen) {
		t.Fatalf("second decision = %v, want ErrNotOpen", again)
	}
	if !errors.Is(missing, store.ErrNotFound) || !errors.Is(foreign, store.ErrNotFound) {
		t.Fatalf("missing %v, foreign %v; want ErrNotFound", missing, foreign)
	}
	got, _ := repo.GetSuggestion(ctx, owner, row.ID)
	if got.State != "accepted" {
		t.Fatalf("state = %s, want the first decision kept", got.State)
	}
}

func TestListSuggestionsPagesNewestFirstAndFilters(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	var ids []uuid.UUID
	for i := range 5 {
		target := domain.TargetResume
		if i%2 == 1 {
			target = domain.TargetLinkedIn
		}
		ids = append(ids, add(t, repo, owner, target, i).ID)
	}
	add(t, repo, uuid.New(), domain.TargetResume, 1)
	if _, err := repo.Decide(ctx, owner, ids[0], domain.StateDismissed, base); err != nil {
		t.Fatalf("decide: %v", err)
	}

	var got []uuid.UUID
	token := ""
	for pages := 0; ; pages++ {
		rows, next, err := repo.ListSuggestions(ctx, owner, store.SuggestionFilter{}, store.Page{Size: 2, Token: token})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, r := range rows {
			got = append(got, r.ID)
		}
		if next == "" {
			if pages != 2 {
				t.Fatalf("pages = %d, want 3", pages+1)
			}
			break
		}
		token = next
	}
	open, dismissed, linkedin := domain.StateOpen, domain.StateDismissed, domain.TargetLinkedIn
	openRows, _, openErr := repo.ListSuggestions(ctx, owner, store.SuggestionFilter{State: &open}, store.Page{})
	dismissedRows, _, _ := repo.ListSuggestions(ctx, owner, store.SuggestionFilter{State: &dismissed}, store.Page{})
	linkedinRows, _, _ := repo.ListSuggestions(ctx, owner, store.SuggestionFilter{Target: &linkedin}, store.Page{})
	_, _, tokenErr := repo.ListSuggestions(ctx, owner, store.SuggestionFilter{}, store.Page{Token: "!!"})

	slices.Reverse(ids)
	if !slices.Equal(got, ids) {
		t.Fatalf("paged = %v, want %v", got, ids)
	}
	if openErr != nil || len(openRows) != 4 || len(dismissedRows) != 1 || len(linkedinRows) != 2 {
		t.Fatalf("open %d, dismissed %d, linkedin %d, err %v", len(openRows), len(dismissedRows), len(linkedinRows), openErr)
	}
	if !errors.Is(tokenErr, store.ErrInvalidPageToken) {
		t.Fatalf("token err = %v", tokenErr)
	}
}

func TestSnapshotsComeBackNewestFirstPerOwner(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	insert := func(owner uuid.UUID, i int, etag string) db.GithubSnapshot {
		row, err := repo.InsertSnapshot(ctx, store.NewSnapshot{
			ID: store.NewID(), OwnerID: owner, TakenAt: base.Add(time.Duration(i) * time.Hour),
			Repos: []byte(`[{"name":"r"}]`), Contributions: []byte(`{}`), ETag: etag,
		})
		if err != nil {
			t.Fatalf("insert snapshot: %v", err)
		}
		return row
	}
	oldest, middle, newest := insert(owner, 0, ""), insert(owner, 1, `W/"a"`), insert(owner, 2, `W/"b"`)
	insert(uuid.New(), 3, "other")

	rows, err := repo.LatestSnapshots(ctx, owner, 2)
	all, _ := repo.LatestSnapshots(ctx, owner, 10)

	if err != nil || len(rows) != 2 || rows[0].ID != newest.ID || rows[1].ID != middle.ID {
		t.Fatalf("rows = %v, %v", rows, err)
	}
	if len(all) != 3 || all[2].ID != oldest.ID || all[2].Etag != nil || rows[0].Etag == nil || *rows[0].Etag != `W/"b"` {
		t.Fatalf("all = %d, etags %v / %v", len(all), all[2].Etag, rows[0].Etag)
	}
}

func TestMigrationsEnforceTheChecks(t *testing.T) {
	_, pool := newRepo(t)
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"known target", `INSERT INTO suggestions (id, owner_id, target, section, after, reason) VALUES (gen_random_uuid(), gen_random_uuid(), 'resume', 's', 'a', 'r')`, false},
		{"unknown target", `INSERT INTO suggestions (id, owner_id, target, section, after, reason) VALUES (gen_random_uuid(), gen_random_uuid(), 'cv', 's', 'a', 'r')`, true},
		{"unknown state", `INSERT INTO suggestions (id, owner_id, target, section, after, reason, state) VALUES (gen_random_uuid(), gen_random_uuid(), 'resume', 's', 'a', 'r', 'maybe')`, true},
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
