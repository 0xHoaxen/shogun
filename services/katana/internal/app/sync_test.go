package app_test

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/0xHoaxen/shogun/pkg/owners"
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

// connect gives the owner a first snapshot, which is how the shared GitHub
// account is tied to its one owner until each user connects their own.
func connect(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID) {
	t.Helper()
	if _, err := store.New(pool).InsertSnapshot(context.Background(), store.NewSnapshot{
		ID: store.NewID(), OwnerID: owner, TakenAt: testNow.Add(-24 * time.Hour), Repos: []byte(`[]`), Contributions: []byte(`{}`),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestAnOwnerWithoutASnapshotCannotUseTheSharedGitHubAccount(t *testing.T) {
	// Arrange: a second user signs up and asks for a sync.
	pool, stranger := newPool(t), uuid.New()
	gh := &fakeGitHub{snap: snapshot(`W/"a"`)}
	svc := app.NewService(pool, gh, func() time.Time { return testNow })

	// Act
	_, err := svc.SyncGitHub(asOwner(stranger))

	// Assert
	if !errors.Is(err, app.ErrGitHubNotConnected) {
		t.Fatalf("err = %v, want ErrGitHubNotConnected", err)
	}
	if len(gh.etags) != 0 || count(t, pool) != 0 {
		t.Fatalf("GitHub was called %d times and %d snapshots stored; the operator's account must not be read for another user", len(gh.etags), count(t, pool))
	}
}

func TestSyncStoresASnapshotWithItsETag(t *testing.T) {
	pool, owner := newPool(t), uuid.New()
	connect(t, pool, owner)
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
	connect(t, pool, owner)
	gh := &fakeGitHub{snap: snapshot(`W/"a"`)}
	svc := app.NewService(pool, gh, func() time.Time { return testNow })
	if _, err := svc.SyncGitHub(asOwner(owner)); err != nil {
		t.Fatalf("first: %v", err)
	}

	res, err := svc.SyncGitHub(asOwner(owner))

	if err != nil || res.Changed {
		t.Fatalf("result %+v, err %v; want unchanged", res, err)
	}
	if len(gh.etags) != 2 || gh.etags[0] != "" || gh.etags[1] != `W/"a"` || count(t, pool) != 2 {
		t.Fatalf("etags %v, snapshots %d; want the stored etag sent and one snapshot beyond the first", gh.etags, count(t, pool))
	}
}

func TestANewETagStoresASecondSnapshot(t *testing.T) {
	pool, owner := newPool(t), uuid.New()
	connect(t, pool, owner)
	gh := &fakeGitHub{snap: snapshot(`W/"a"`)}
	svc := app.NewService(pool, gh, func() time.Time { return testNow })
	_, _ = svc.SyncGitHub(asOwner(owner))
	gh.snap = snapshot(`W/"b"`)

	res, err := svc.SyncGitHub(asOwner(owner))

	if err != nil || !res.Changed || count(t, pool) != 3 {
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
		pool, owner := newPool(t), uuid.New()
		connect(t, pool, owner)
		svc := app.NewService(pool, &fakeGitHub{err: cause}, nil)

		_, err := svc.SyncGitHub(asOwner(owner))

		if err == nil || count(t, pool) != 1 {
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

func TestSyncableOwnersAreTheActiveOwnersWithAConnection(t *testing.T) {
	// Arrange
	ctx := context.Background()
	pool := newPool(t)
	connected, noConnection, disabled := uuid.New(), uuid.New(), uuid.New()
	connect(t, pool, connected)
	connect(t, pool, disabled)
	for _, o := range []owners.Owner{
		{ID: connected, Timezone: "Asia/Kolkata", Status: owners.StatusActive, Version: 1},
		{ID: noConnection, Timezone: "Asia/Kolkata", Status: owners.StatusActive, Version: 1},
		{ID: disabled, Timezone: "Asia/Kolkata", Status: owners.StatusDisabled, Version: 1},
	} {
		if err := owners.Upsert(ctx, pool, o); err != nil {
			t.Fatalf("upsert owner: %v", err)
		}
	}
	svc := app.NewService(pool, &fakeGitHub{}, nil)

	// Act
	got, err := svc.SyncableOwners(ctx)

	// Assert
	if err != nil || len(got) != 1 || got[0].ID != connected {
		t.Fatalf("SyncableOwners = %+v, %v; want only the active owner who has synced, not a new user or a disabled one", got, err)
	}
}

func TestNoOwnerIsSyncableWithoutAGitHubAccountConfigured(t *testing.T) {
	ctx := context.Background()
	pool, owner := newPool(t), uuid.New()
	connect(t, pool, owner)
	if err := owners.Upsert(ctx, pool, owners.Owner{ID: owner, Timezone: "UTC", Status: owners.StatusActive, Version: 1}); err != nil {
		t.Fatalf("upsert owner: %v", err)
	}

	got, err := app.NewService(pool, nil, nil).SyncableOwners(ctx)

	if err != nil || len(got) != 0 {
		t.Fatalf("SyncableOwners = %+v, %v; want none", got, err)
	}
}

func TestSyncForReadsGitHubForThatOwnerAlone(t *testing.T) {
	pool := newPool(t)
	first, second := uuid.New(), uuid.New()
	connect(t, pool, first)
	connect(t, pool, second)
	gh := &fakeGitHub{snap: snapshot(`W/"new"`)}
	svc := app.NewService(pool, gh, func() time.Time { return testNow })

	res, err := svc.SyncFor(context.Background(), first)

	if err != nil || !res.Changed || count(t, pool) != 3 || len(gh.etags) != 1 {
		t.Fatalf("res %+v, err %v, snapshots %d, calls %d; want one new snapshot for the one owner", res, err, count(t, pool), len(gh.etags))
	}
	if rows, _ := store.New(pool).LatestSnapshots(context.Background(), second, 5); len(rows) != 1 {
		t.Fatalf("the other owner has %d snapshots, want only their first", len(rows))
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
	svc := app.NewService(pool, github.New(srv.URL, "octo", token, nil), nil)

	_, allErr := svc.SyncFor(context.Background(), owner)
	_, oneErr := svc.SyncGitHub(asOwner(owner))

	for _, text := range []string{allErr.Error(), oneErr.Error()} {
		if strings.Contains(text, token) {
			t.Fatalf("token leaked: %q", text)
		}
	}
	if !errors.Is(allErr, app.ErrGitHubUnauthorized) {
		t.Fatalf("err = %v", allErr)
	}
}
