package app_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/github"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
	"github.com/0xHoaxen/shogun/services/katana/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

var testNow = time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)

// fakeGitHub answers Fetch from a script and records the ETags it was given.
type fakeGitHub struct {
	mu    sync.Mutex
	snap  domain.Snapshot
	err   error
	etags []string
}

func (f *fakeGitHub) Fetch(_ context.Context, etag string) (domain.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.etags = append(f.etags, etag)
	if f.err != nil {
		return domain.Snapshot{}, f.err
	}
	if etag != "" && etag == f.snap.ETag {
		return domain.Snapshot{}, domain.ErrNotModified
	}
	return f.snap, nil
}

func newPool(t *testing.T) *pgxpool.Pool {
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
	return pool
}

func asOwner(owner uuid.UUID) context.Context {
	return authz.WithIdentity(context.Background(), authz.Identity{OwnerID: owner.String(), RequestID: "r"})
}

func snapshot(etag string) domain.Snapshot {
	return domain.Snapshot{
		ETag:  etag,
		Repos: []domain.Repo{{Name: "shogun", Language: "Go", Stars: 3}},
		Contributions: domain.Contributions{MergedPRs: []domain.PullRequest{
			{Repo: "x/y", Title: "Add worker pool", URL: "https://github.com/x/y/pull/1", MergedAt: testNow},
		}},
	}
}

func count(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM github_snapshots`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestSyncStoresASnapshotWithItsETag(t *testing.T) {
	pool, owner := newPool(t), uuid.New()
	gh := &fakeGitHub{snap: snapshot(`W/"a"`)}
	svc := app.NewService(pool, gh, func() time.Time { return testNow })

	res, err := svc.SyncGitHub(asOwner(owner))

	if err != nil || !res.Changed || res.SnapshotID == uuid.Nil {
		t.Fatalf("result %+v, err %v", res, err)
	}
	rows, _ := store.New(pool).LatestSnapshots(context.Background(), owner, 1)
	if len(rows) != 1 || rows[0].ID != res.SnapshotID || rows[0].Etag == nil || *rows[0].Etag != `W/"a"` || !rows[0].TakenAt.Equal(testNow) {
		t.Fatalf("rows = %+v", rows)
	}
	if !bytes.Contains(rows[0].Repos, []byte(`"shogun"`)) || !bytes.Contains(rows[0].Contributions, []byte(`Add worker pool`)) {
		t.Fatalf("stored repos %s, contributions %s", rows[0].Repos, rows[0].Contributions)
	}
}

func TestASecondSyncSendsTheETagAndStoresNothingWhenNotModified(t *testing.T) {
	pool, owner := newPool(t), uuid.New()
	gh := &fakeGitHub{snap: snapshot(`W/"a"`)}
	svc := app.NewService(pool, gh, func() time.Time { return testNow })
	if _, err := svc.SyncGitHub(asOwner(owner)); err != nil {
		t.Fatalf("first: %v", err)
	}

	res, err := svc.SyncGitHub(asOwner(owner))

	if err != nil || res.Changed {
		t.Fatalf("result %+v, err %v; want unchanged", res, err)
	}
	if len(gh.etags) != 2 || gh.etags[0] != "" || gh.etags[1] != `W/"a"` || count(t, pool) != 1 {
		t.Fatalf("etags %v, snapshots %d; want the stored etag sent and one snapshot", gh.etags, count(t, pool))
	}
}

func TestANewETagStoresASecondSnapshot(t *testing.T) {
	pool, owner := newPool(t), uuid.New()
	gh := &fakeGitHub{snap: snapshot(`W/"a"`)}
	svc := app.NewService(pool, gh, func() time.Time { return testNow })
	_, _ = svc.SyncGitHub(asOwner(owner))
	gh.snap = snapshot(`W/"b"`)

	res, err := svc.SyncGitHub(asOwner(owner))

	if err != nil || !res.Changed || count(t, pool) != 2 {
		t.Fatalf("result %+v, err %v, snapshots %d", res, err, count(t, pool))
	}
}

func TestSyncRefusesACallWithoutAnOwnerAndAnUnconfiguredGitHub(t *testing.T) {
	pool := newPool(t)

	_, noOwner := app.NewService(pool, &fakeGitHub{}, nil).SyncGitHub(context.Background())
	_, unset := app.NewService(pool, nil, nil).SyncGitHub(asOwner(uuid.New()))

	if !errors.Is(noOwner, app.ErrNoOwner) || !errors.Is(unset, app.ErrGitHubNotConfigured) {
		t.Fatalf("no owner: %v, unset: %v", noOwner, unset)
	}
}

func TestSyncPassesGitHubFailuresOnAndStoresNothing(t *testing.T) {
	for _, cause := range []error{domain.ErrUnauthorized, domain.ErrUnavailable, &domain.RateLimitError{ResetAt: testNow}} {
		pool := newPool(t)
		svc := app.NewService(pool, &fakeGitHub{err: cause}, nil)

		_, err := svc.SyncGitHub(asOwner(uuid.New()))

		if err == nil || count(t, pool) != 0 {
			t.Fatalf("cause %v: err %v, snapshots %d", cause, err, count(t, pool))
		}
		var limit *app.RateLimitError
		if errors.Is(cause, domain.ErrUnauthorized) && !errors.Is(err, app.ErrGitHubUnauthorized) ||
			errors.Is(cause, domain.ErrUnavailable) && !errors.Is(err, app.ErrGitHubUnavailable) ||
			errors.As(cause, &limit) && !errors.As(err, &limit) {
			t.Fatalf("cause %v lost on the way: %v", cause, err)
		}
	}
}

func TestSyncAllCoversEveryOwnerWithASnapshotAndKeepsGoingAfterAFailure(t *testing.T) {
	pool := newPool(t)
	repo := store.New(pool)
	owners := []uuid.UUID{uuid.New(), uuid.New()}
	for _, o := range owners {
		if _, err := repo.InsertSnapshot(context.Background(), store.NewSnapshot{
			ID: store.NewID(), OwnerID: o, TakenAt: testNow.Add(-24 * time.Hour), Repos: []byte(`[]`), Contributions: []byte(`{}`), ETag: "old",
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	gh := &fakeGitHub{snap: snapshot(`W/"new"`)}
	svc := app.NewService(pool, gh, func() time.Time { return testNow })

	err := svc.SyncAll(context.Background(), slog.New(slog.DiscardHandler))

	if err != nil || count(t, pool) != 4 {
		t.Fatalf("err %v, snapshots %d; want one new snapshot per owner", err, count(t, pool))
	}
	gh.err = domain.ErrUnavailable
	failed := svc.SyncAll(context.Background(), slog.New(slog.DiscardHandler))
	if failed == nil || len(gh.etags) != 4 {
		t.Fatalf("err %v, calls %d; want both owners tried and an error", failed, len(gh.etags))
	}
	unset := app.NewService(pool, nil, nil).SyncAll(context.Background(), slog.New(slog.DiscardHandler))
	if !errors.Is(unset, app.ErrGitHubNotConfigured) {
		t.Fatalf("unconfigured SyncAll = %v", unset)
	}
}

func TestSyncNeverPutsTheTokenInAnErrorOrALog(t *testing.T) {
	const token = "ghp_super-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer srv.Close()
	pool, owner := newPool(t), uuid.New()
	if _, err := store.New(pool).InsertSnapshot(context.Background(), store.NewSnapshot{
		ID: store.NewID(), OwnerID: owner, TakenAt: testNow, Repos: []byte(`[]`), Contributions: []byte(`{}`),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	svc := app.NewService(pool, github.New(srv.URL, "octo", token, nil), nil)

	allErr := svc.SyncAll(context.Background(), log)
	_, oneErr := svc.SyncGitHub(asOwner(owner))

	for _, text := range []string{logs.String(), allErr.Error(), oneErr.Error()} {
		if strings.Contains(text, token) {
			t.Fatalf("token leaked: %q", text)
		}
	}
	if !errors.Is(allErr, app.ErrGitHubUnauthorized) {
		t.Fatalf("err = %v", allErr)
	}
}
