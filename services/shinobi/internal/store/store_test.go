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
	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
	"github.com/0xHoaxen/shogun/services/shinobi/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

var base = time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)

func newRepo(t *testing.T) (*store.Repo, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "shinobi")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.New(pool), pool
}

func sourceInput(name string) domain.SourceInput {
	return domain.SourceInput{
		Name: name, Kind: domain.KindRSS, Schedule: domain.DefaultSchedule, Enabled: true,
		Config: domain.SourceConfig{URL: "https://jobs.example.com/" + name},
	}
}

func addSource(t *testing.T, repo *store.Repo, owner uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := store.NewID()
	if _, err := repo.InsertSource(context.Background(), store.NewSource{ID: id, OwnerID: owner, Input: sourceInput(name)}); err != nil {
		t.Fatalf("insert source %q: %v", name, err)
	}
	return id
}

func addPosting(t *testing.T, repo *store.Repo, owner, source uuid.UUID, external, title string) uuid.UUID {
	t.Helper()
	row, err := repo.UpsertPosting(context.Background(), store.NewPosting{
		ID: store.NewID(), OwnerID: owner, SourceID: source, CreatedAt: base, Raw: []byte(`{}`),
		Candidate: domain.Candidate{ExternalID: external, Title: title, Company: "Acme", URL: "https://acme.example/" + external},
	})
	if err != nil {
		t.Fatalf("upsert posting %q: %v", external, err)
	}
	return row.ID
}

func TestSourcesRoundTripAndAreScopedToTheirOwner(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	in := domain.SourceInput{
		Name: "API", Kind: domain.KindAPI, Schedule: "30 6 * * *", Enabled: true,
		Config: domain.SourceConfig{URL: "https://jobs.example.com/api", Mapping: &domain.FieldMapping{ID: "id", Title: "title", ItemsPath: "data"}},
	}
	id := store.NewID()

	row, err := repo.InsertSource(ctx, store.NewSource{ID: id, OwnerID: owner, Input: in})
	cfg, cfgErr := store.ConfigOf(row)
	_, otherErr := repo.GetSource(ctx, uuid.New(), id)

	if err != nil || cfgErr != nil || row.Kind != "api" || row.Schedule != "30 6 * * *" || row.LastRunAt != nil || row.LastError != nil {
		t.Fatalf("row %+v, %v %v", row, err, cfgErr)
	}
	if cfg.URL != in.Config.URL || cfg.Mapping == nil || cfg.Mapping.ItemsPath != "data" {
		t.Fatalf("config = %+v", cfg)
	}
	if !errors.Is(otherErr, store.ErrNotFound) {
		t.Fatalf("another owner's get = %v, want ErrNotFound", otherErr)
	}
}

func TestUpdateSourceKeepsItsKind(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	id := addSource(t, repo, owner, "feed")
	changed := sourceInput("renamed")
	changed.Kind = domain.KindFile // must be ignored
	changed.Enabled = false
	changed.Schedule = "0 9 * * *"

	row, err := repo.UpdateSource(ctx, owner, id, changed)
	_, missing := repo.UpdateSource(ctx, owner, uuid.New(), changed)
	_, foreign := repo.UpdateSource(ctx, uuid.New(), id, changed)

	if err != nil || row.Name != "renamed" || row.Kind != "rss" || row.Enabled || row.Schedule != "0 9 * * *" {
		t.Fatalf("row %+v, %v", row, err)
	}
	if !errors.Is(missing, store.ErrNotFound) || !errors.Is(foreign, store.ErrNotFound) {
		t.Fatalf("missing %v, foreign %v", missing, foreign)
	}
}

func TestListAndEnabledSources(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	b := addSource(t, repo, owner, "b")
	a := addSource(t, repo, owner, "a")
	off := sourceInput("off")
	off.Enabled = false
	if _, err := repo.InsertSource(ctx, store.NewSource{ID: store.NewID(), OwnerID: owner, Input: off}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	addSource(t, repo, uuid.New(), "someone else's")

	listed, err := repo.ListSources(ctx, owner)
	enabled, enabledErr := repo.EnabledSources(ctx)

	if err != nil || len(listed) != 3 || listed[0].ID != a || listed[1].ID != b {
		t.Fatalf("listed %+v, %v; want a, b, off by name", listed, err)
	}
	if enabledErr != nil || len(enabled) != 3 {
		t.Fatalf("enabled = %d, %v; want the two of owner and the other's", len(enabled), enabledErr)
	}
}

func TestMarkSourceRunRecordsTheTimeAndTheFailure(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	id := addSource(t, repo, owner, "feed")

	if err := repo.MarkSourceRun(ctx, id, base, "fetch_failed"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	failed, _ := repo.GetSource(ctx, owner, id)
	if err := repo.MarkSourceRun(ctx, id, base.Add(time.Hour), ""); err != nil {
		t.Fatalf("mark: %v", err)
	}
	ok, _ := repo.GetSource(ctx, owner, id)

	if failed.LastError == nil || *failed.LastError != "fetch_failed" || failed.LastRunAt == nil || !failed.LastRunAt.Equal(base) {
		t.Fatalf("failed run = %+v", failed)
	}
	if ok.LastError != nil || !ok.LastRunAt.Equal(base.Add(time.Hour)) {
		t.Fatalf("good run = %+v; want the error cleared", ok)
	}
}

func TestUpsertPostingInsertsOnceThenRefreshes(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	source := addSource(t, repo, owner, "feed")
	otherSource := addSource(t, repo, owner, "other")
	upsert := func(source uuid.UUID, title string) bool {
		row, err := repo.UpsertPosting(ctx, store.NewPosting{
			ID: store.NewID(), OwnerID: owner, SourceID: source, CreatedAt: base, Raw: []byte(`{"v":1}`),
			Candidate: domain.Candidate{ExternalID: "42", Title: title},
		})
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		return row.Inserted
	}

	first, again, elsewhere := upsert(source, "Backend Engineer"), upsert(source, "Senior Backend Engineer"), upsert(otherSource, "Backend Engineer")

	rows, _, _ := repo.ListPostings(ctx, owner, store.PostingFilter{SourceID: &source}, store.Page{})
	if !first || again || !elsewhere || len(rows) != 1 || rows[0].Title != "Senior Backend Engineer" {
		t.Fatalf("first %v again %v elsewhere %v, rows %+v; want one refreshed posting per source", first, again, elsewhere, rows)
	}
}

func score(v float32) domain.Score { return domain.Score{Value: v, Reasons: []string{"because"}} }

func TestListPostingsOrdersByScoreAndPagesAcrossUnscored(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	source := addSource(t, repo, owner, "feed")
	var want []uuid.UUID
	scores := []float32{0.9, 0.8, 0.8, 0.3}
	for i, v := range scores {
		id := addPosting(t, repo, owner, source, string(rune('a'+i)), "scored")
		if err := repo.SaveScore(ctx, id, score(v), "rule", base); err != nil {
			t.Fatalf("save score: %v", err)
		}
	}
	unscoredA, unscoredB := addPosting(t, repo, owner, source, "u1", "unscored"), addPosting(t, repo, owner, source, "u2", "unscored")
	addPosting(t, repo, uuid.New(), addSource(t, repo, uuid.New(), "x"), "z", "someone else's")

	var got []uuid.UUID
	var gotScores []float32
	token := ""
	for pages := 0; ; pages++ {
		rows, next, err := repo.ListPostings(ctx, owner, store.PostingFilter{}, store.Page{Size: 2, Token: token})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, r := range rows {
			got = append(got, r.ID)
			if r.Score != nil {
				gotScores = append(gotScores, *r.Score)
			}
		}
		if next == "" {
			if pages != 2 {
				t.Fatalf("pages = %d, want 3", pages+1)
			}
			break
		}
		token = next
	}
	_ = want

	if !slices.Equal(gotScores, []float32{0.9, 0.8, 0.8, 0.3}) || len(got) != 6 {
		t.Fatalf("scores %v over %d rows; want best first and every row once", gotScores, len(got))
	}
	if tail := got[4:]; !slices.Contains(tail, unscoredA) || !slices.Contains(tail, unscoredB) {
		t.Fatalf("tail = %v, want both unscored postings last", tail)
	}
	if len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Fatalf("a posting appeared twice: %v", got)
	}
}

func TestListPostingsFiltersByMinScoreAndSource(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	one, two := addSource(t, repo, owner, "one"), addSource(t, repo, owner, "two")
	high := addPosting(t, repo, owner, one, "h", "high")
	low := addPosting(t, repo, owner, one, "l", "low")
	other := addPosting(t, repo, owner, two, "o", "other")
	addPosting(t, repo, owner, one, "u", "unscored")
	for id, v := range map[uuid.UUID]float32{high: 0.9, low: 0.2, other: 0.95} {
		if err := repo.SaveScore(ctx, id, score(v), "rule", base); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	floor := float32(0.5)

	good, _, err := repo.ListPostings(ctx, owner, store.PostingFilter{MinScore: &floor}, store.Page{})
	inOne, _, _ := repo.ListPostings(ctx, owner, store.PostingFilter{SourceID: &one}, store.Page{})
	_, _, tokenErr := repo.ListPostings(ctx, owner, store.PostingFilter{}, store.Page{Token: "!!"})

	if err != nil || len(good) != 2 || good[0].ID != other || good[1].ID != high {
		t.Fatalf("min score rows = %+v, %v; want only the scored ones at or above it", good, err)
	}
	if len(inOne) != 3 {
		t.Fatalf("source one rows = %d, want 3", len(inOne))
	}
	if !errors.Is(tokenErr, store.ErrInvalidPageToken) {
		t.Fatalf("token err = %v", tokenErr)
	}
}

func TestSaveScoreReplacesTheRuleScoreWithTheModels(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	id := addPosting(t, repo, owner, addSource(t, repo, owner, "feed"), "1", "x")
	if _, err := repo.GetScore(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("score before = %v, want ErrNotFound", err)
	}

	_ = repo.SaveScore(ctx, id, domain.Score{Value: 0.55, Reasons: []string{"rule says so"}}, "rule", base)
	_ = repo.SaveScore(ctx, id, domain.Score{Value: 0.8}, "llm", base.Add(time.Hour))

	got, err := repo.GetScore(ctx, id)
	if err != nil || got.Score != 0.8 || got.ScoredBy != "llm" || len(store.ReasonsOf(got.Reasons)) != 0 {
		t.Fatalf("score = %+v, %v", got, err)
	}
}

func TestMarkMatchedSucceedsOnlyOnceAndSurvivesARescore(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	id := addPosting(t, repo, owner, addSource(t, repo, owner, "feed"), "1", "x")
	_ = repo.SaveScore(ctx, id, score(0.9), "rule", base)

	first, firstErr := repo.MarkMatched(ctx, id, base)
	again, againErr := repo.MarkMatched(ctx, id, base.Add(time.Hour))
	_ = repo.SaveScore(ctx, id, score(0.4), "llm", base.Add(2*time.Hour)) // a later score does not reset it
	afterRescore, rescoreErr := repo.MarkMatched(ctx, id, base.Add(3*time.Hour))
	noScore, noScoreErr := repo.MarkMatched(ctx, uuid.New(), base)

	if !first || firstErr != nil || again || againErr != nil || afterRescore || rescoreErr != nil || noScore || noScoreErr != nil {
		t.Fatalf("first %v %v, again %v %v, after a rescore %v %v, no score %v %v", first, firstErr, again, againErr, afterRescore, rescoreErr, noScore, noScoreErr)
	}
}

func TestUnscoredPostingIDsListsOnlyThoseWithoutAScore(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	source := addSource(t, repo, owner, "feed")
	scored := addPosting(t, repo, owner, source, "1", "x")
	open := addPosting(t, repo, owner, source, "2", "y")
	_ = repo.SaveScore(ctx, scored, score(0.5), "rule", base)

	ids, err := repo.UnscoredPostingIDs(ctx, source)

	if err != nil || !slices.Equal(ids, []uuid.UUID{open}) {
		t.Fatalf("ids = %v, %v", ids, err)
	}
}

func TestSetSavedJobRecordsTheFirstJobOnly(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()
	id := addPosting(t, repo, owner, addSource(t, repo, owner, "feed"), "1", "x")
	first, second := uuid.New(), uuid.New()

	row, set, err := repo.SetSavedJob(ctx, owner, id, first)
	again, setAgain, againErr := repo.SetSavedJob(ctx, owner, id, second)
	_, _, foreign := repo.SetSavedJob(ctx, uuid.New(), id, second)

	if err != nil || !set || row.SavedJobID == nil || *row.SavedJobID != first {
		t.Fatalf("first = %+v %v %v", row, set, err)
	}
	if againErr != nil || setAgain || again.SavedJobID == nil || *again.SavedJobID != first {
		t.Fatalf("second = %+v %v %v; want the first job kept", again, setAgain, againErr)
	}
	if !errors.Is(foreign, store.ErrNotFound) {
		t.Fatalf("foreign = %v, want ErrNotFound", foreign)
	}
}

func TestPreferencesDefaultThenRoundTrip(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	owner := uuid.New()

	before, err := repo.GetPreferences(ctx, owner)
	saved, setErr := repo.SetPreferences(ctx, owner, domain.Preferences{
		Roles: []string{"backend engineer"}, Exclude: []string{"unpaid"}, MinScore: 0.6,
	}, base)
	after, getErr := repo.GetPreferences(ctx, owner)
	other, _ := repo.GetPreferences(ctx, uuid.New())

	if err != nil || !before.Empty() || before.MinScore != domain.DefaultMinScore {
		t.Fatalf("before = %+v, %v", before, err)
	}
	if setErr != nil || getErr != nil || !slices.Equal(after.Roles, []string{"backend engineer"}) || after.MinScore != 0.6 ||
		after.Locations == nil || len(saved.Exclude) != 1 {
		t.Fatalf("saved %+v, after %+v, %v %v", saved, after, setErr, getErr)
	}
	if !other.Empty() {
		t.Fatalf("another owner sees %+v", other)
	}
}

func TestMigrationsEnforceTheChecks(t *testing.T) {
	_, pool := newRepo(t)
	const source = `WITH s AS (INSERT INTO sources (id, owner_id, name, kind, config) VALUES (gen_random_uuid(), gen_random_uuid(), 'n', 'rss', '{}') RETURNING id),
		p AS (INSERT INTO postings (id, owner_id, source_id, external_id, title, raw) SELECT gen_random_uuid(), gen_random_uuid(), id, 'e', 't', '{}' FROM s RETURNING id) `
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"known kind", `INSERT INTO sources (id, owner_id, name, kind, config) VALUES (gen_random_uuid(), gen_random_uuid(), 'n', 'api', '{}')`, false},
		{"unknown kind", `INSERT INTO sources (id, owner_id, name, kind, config) VALUES (gen_random_uuid(), gen_random_uuid(), 'n', 'html', '{}')`, true},
		{"good score", source + `INSERT INTO scores (posting_id, score, reasons, scored_by) SELECT id, 0.5, '[]', 'rule' FROM p`, false},
		{"score above one", source + `INSERT INTO scores (posting_id, score, reasons, scored_by) SELECT id, 1.5, '[]', 'rule' FROM p`, true},
		{"unknown scorer", source + `INSERT INTO scores (posting_id, score, reasons, scored_by) SELECT id, 0.5, '[]', 'human' FROM p`, true},
		{"min score out of range", `INSERT INTO preferences (owner_id, min_score) VALUES (gen_random_uuid(), 2)`, true},
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
