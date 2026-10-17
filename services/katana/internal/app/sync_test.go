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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/envelope"
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
	creds []domain.Credentials
}

func (f *fakeGitHub) Fetch(_ context.Context, creds domain.Credentials, etag string) (domain.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.etags = append(f.etags, etag)
	f.creds = append(f.creds, creds)
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

// testKeys seals GitHub tokens in tests.
func testKeys(t *testing.T) *envelope.Keyring {
	t.Helper()
	keys, err := envelope.NewKeyring("katana-test", map[string][]byte{"katana-test": bytes.Repeat([]byte("k"), envelope.KeySize)})
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	return keys
}

// newSvc is a service that can seal GitHub tokens.
func newSvc(t *testing.T, pool *pgxpool.Pool, gh app.GitHub, now func() time.Time, opts ...app.Option) *app.Service {
	t.Helper()
	return app.NewService(pool, gh, now, append([]app.Option{app.WithKeyring(testKeys(t))}, opts...)...)
}

const testToken = "ghp_owner-token-0123"

// connect gives the owner a GitHub connection of their own: login "octo-<id>"
// and a sealed token.
func connect(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID) {
	t.Helper()
	if _, err := newSvc(t, pool, nil, nil).SaveGitHubAccount(asOwner(owner), "octo-"+owner.String()[:8], testToken); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

func TestAnOwnerWhoHasNotConnectedGitHubIsNeverReadWithAnyoneElsesAccount(t *testing.T) {
	// Arrange: a user who signed up and never connected GitHub asks for a sync, while another user is connected.
	pool, stranger := newPool(t), uuid.New()
	connect(t, pool, uuid.New())
	gh := &fakeGitHub{snap: snapshot(`W/"a"`)}
	svc := newSvc(t, pool, gh, func() time.Time { return testNow })

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
	svc := newSvc(t, pool, gh, func() time.Time { return testNow })

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
	svc := newSvc(t, pool, gh, func() time.Time { return testNow })
	if _, err := svc.SyncGitHub(asOwner(owner)); err != nil {
		t.Fatalf("first: %v", err)
	}

	res, err := svc.SyncGitHub(asOwner(owner))

	if err != nil || res.Changed {
		t.Fatalf("result %+v, err %v; want unchanged", res, err)
	}
	if len(gh.etags) != 2 || gh.etags[0] != "" || gh.etags[1] != `W/"a"` || count(t, pool) != 1 {
		t.Fatalf("etags %v, snapshots %d; want the stored etag sent and the one snapshot", gh.etags, count(t, pool))
	}
}

func TestANewETagStoresASecondSnapshot(t *testing.T) {
	pool, owner := newPool(t), uuid.New()
	connect(t, pool, owner)
	gh := &fakeGitHub{snap: snapshot(`W/"a"`)}
	svc := newSvc(t, pool, gh, func() time.Time { return testNow })
	_, _ = svc.SyncGitHub(asOwner(owner))
	gh.snap = snapshot(`W/"b"`)

	res, err := svc.SyncGitHub(asOwner(owner))

	if err != nil || !res.Changed || count(t, pool) != 2 {
		t.Fatalf("result %+v, err %v, snapshots %d", res, err, count(t, pool))
	}
}

func TestSyncRefusesACallWithoutAnOwnerAndAnUnconfiguredGitHub(t *testing.T) {
	pool := newPool(t)

	_, noOwner := newSvc(t, pool, &fakeGitHub{}, nil).SyncGitHub(context.Background())
	_, unset := newSvc(t, pool, nil, nil).SyncGitHub(asOwner(uuid.New()))

	if !errors.Is(noOwner, app.ErrNoOwner) || !errors.Is(unset, app.ErrGitHubNotConfigured) {
		t.Fatalf("no owner: %v, unset: %v", noOwner, unset)
	}
}

func TestSyncPassesGitHubFailuresOnAndStoresNothing(t *testing.T) {
	for _, cause := range []error{domain.ErrUnauthorized, domain.ErrUnavailable, &domain.RateLimitError{ResetAt: testNow}} {
		pool, owner := newPool(t), uuid.New()
		connect(t, pool, owner)
		svc := newSvc(t, pool, &fakeGitHub{err: cause}, nil)

		_, err := svc.SyncGitHub(asOwner(owner))

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
	svc := newSvc(t, pool, &fakeGitHub{}, nil)

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

	got, err := newSvc(t, pool, nil, nil).SyncableOwners(ctx)

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
	svc := newSvc(t, pool, gh, func() time.Time { return testNow })

	res, err := svc.SyncFor(context.Background(), first)

	if err != nil || !res.Changed || count(t, pool) != 1 || len(gh.etags) != 1 {
		t.Fatalf("res %+v, err %v, snapshots %d, calls %d; want one new snapshot for the one owner", res, err, count(t, pool), len(gh.etags))
	}
	if rows, _ := store.New(pool).LatestSnapshots(context.Background(), second, 5); len(rows) != 0 {
		t.Fatalf("the other owner has %d snapshots, want none: only the synced owner is read", len(rows))
	}
	if len(gh.creds) != 1 || gh.creds[0].Token != testToken || gh.creds[0].Login != "octo-"+first.String()[:8] {
		t.Fatalf("GitHub was read with %+v, want the first owner's own login and token", gh.creds)
	}
}

func TestSyncSendsTheOwnersOwnTokenAndNeverPutsItInAnError(t *testing.T) {
	// Arrange: GitHub refuses the token and echoes it back in its answer.
	var sent atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad credentials: " + r.Header.Get("Authorization")))
	}))
	defer srv.Close()
	pool, owner := newPool(t), uuid.New()
	connect(t, pool, owner)
	svc := newSvc(t, pool, github.New(srv.URL, nil), nil)

	// Act
	_, allErr := svc.SyncFor(context.Background(), owner)
	_, oneErr := svc.SyncGitHub(asOwner(owner))

	// Assert
	if sent.Load() != "Bearer "+testToken {
		t.Fatalf("GitHub received %v, want the owner's own token", sent.Load())
	}
	for _, text := range []string{allErr.Error(), oneErr.Error()} {
		if strings.Contains(text, testToken) {
			t.Fatalf("token leaked: %q", text)
		}
	}
	if !errors.Is(allErr, app.ErrGitHubUnauthorized) {
		t.Fatalf("err = %v", allErr)
	}
}

func TestEachOwnerIsReadWithTheirOwnToken(t *testing.T) {
	pool := newPool(t)
	ada, bob := uuid.New(), uuid.New()
	for owner, token := range map[uuid.UUID]string{ada: "ghp_ada-token-0001", bob: "ghp_bob-token-0002"} {
		if _, err := newSvc(t, pool, nil, nil).SaveGitHubAccount(asOwner(owner), "login-"+token[4:7], token); err != nil {
			t.Fatalf("connect: %v", err)
		}
	}
	gh := &fakeGitHub{snap: snapshot(`W/"x"`)}
	svc := newSvc(t, pool, gh, func() time.Time { return testNow })

	_, errAda := svc.SyncFor(context.Background(), ada)
	_, errBob := svc.SyncFor(context.Background(), bob)

	if errAda != nil || errBob != nil || len(gh.creds) != 2 {
		t.Fatalf("errors %v %v, %d reads", errAda, errBob, len(gh.creds))
	}
	if gh.creds[0] != (domain.Credentials{Login: "login-ada", Token: "ghp_ada-token-0001"}) ||
		gh.creds[1] != (domain.Credentials{Login: "login-bob", Token: "ghp_bob-token-0002"}) {
		t.Fatalf("credentials used: %+v; each owner must be read with their own", gh.creds)
	}
}

func TestATokenIsSealedAtRestAndCopyingItToAnotherOwnerDoesNotOpenIt(t *testing.T) {
	pool, owner, thief := newPool(t), uuid.New(), uuid.New()
	connect(t, pool, owner)
	var ciphertext []byte
	if err := pool.QueryRow(context.Background(), `SELECT token_ciphertext FROM github_accounts WHERE owner_id = $1`, owner).Scan(&ciphertext); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO github_accounts (owner_id, login, token_ciphertext, key_id)
		SELECT $1, login, token_ciphertext, key_id FROM github_accounts WHERE owner_id = $2`, thief, owner); err != nil {
		t.Fatalf("copy row: %v", err)
	}
	gh := &fakeGitHub{snap: snapshot(`W/"x"`)}
	svc := newSvc(t, pool, gh, nil)

	_, err := svc.SyncFor(context.Background(), thief)

	if bytes.Contains(ciphertext, []byte(testToken)) {
		t.Fatal("the token is stored in the clear")
	}
	if err == nil || len(gh.creds) != 0 {
		t.Fatalf("err %v, %d GitHub reads; want a copied token to stay sealed", err, len(gh.creds))
	}
}

func TestDisconnectingRemovesTheTokenAndStopsSyncing(t *testing.T) {
	pool, owner := newPool(t), uuid.New()
	connect(t, pool, owner)
	gh := &fakeGitHub{snap: snapshot(`W/"x"`)}
	svc := newSvc(t, pool, gh, nil)
	if _, err := svc.SyncGitHub(asOwner(owner)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	first := svc.DisconnectGitHub(asOwner(owner))
	again := svc.DisconnectGitHub(asOwner(owner))
	_, view := svc.GitHubAccount(asOwner(owner))
	_, sync := svc.SyncGitHub(asOwner(owner))

	if first != nil || again != nil {
		t.Fatalf("disconnect %v then %v, want both fine", first, again)
	}
	if !errors.Is(view, app.ErrGitHubNotConnected) || !errors.Is(sync, app.ErrGitHubNotConnected) {
		t.Fatalf("account %v, sync %v; want not connected", view, sync)
	}
	if count(t, pool) != 1 {
		t.Fatalf("%d snapshots, want the one already made kept", count(t, pool))
	}
}

func TestGitHubAccountShowsTheLoginAndNeverTheToken(t *testing.T) {
	pool, owner := newPool(t), uuid.New()
	connect(t, pool, owner)

	view, err := newSvc(t, pool, nil, nil).GitHubAccount(asOwner(owner))

	if err != nil || view.Login != "octo-"+owner.String()[:8] || view.ConnectedAt.IsZero() {
		t.Fatalf("view %+v, err %v", view, err)
	}
}

func TestWithoutAKeyringATokenCannotBeStored(t *testing.T) {
	pool := newPool(t)

	_, err := app.NewService(pool, nil, nil).SaveGitHubAccount(asOwner(uuid.New()), "octo", testToken)

	if !errors.Is(err, app.ErrGitHubStorageOff) || strings.Contains(err.Error(), testToken) {
		t.Fatalf("err = %v, want a refusal that does not repeat the token", err)
	}
}
