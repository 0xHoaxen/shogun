package app_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/fetch"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
	"github.com/0xHoaxen/shogun/services/shinobi/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

var testNow = time.Date(2026, 10, 7, 4, 0, 0, 0, time.UTC)

// fakeFetcher answers each address from a script and records what it was asked.
type fakeFetcher struct {
	mu    sync.Mutex
	docs  map[string]string
	err   error
	asked []string
}

func (f *fakeFetcher) Get(_ context.Context, rawURL string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, rawURL)
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.docs[rawURL]), nil
}

func newService(t *testing.T, f *fakeFetcher) (*app.Service, *pgxpool.Pool) {
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
	return app.NewService(pool, f, func() time.Time { return testNow }, slog.New(slog.DiscardHandler)), pool
}

func asOwner(owner uuid.UUID) context.Context {
	return authz.WithIdentity(context.Background(), authz.Identity{OwnerID: owner.String(), RequestID: "r"})
}

const feedURL = "https://jobs.example.com/feed.xml"

const feedXML = `<rss version="2.0"><channel>
 <item><title>Backend Engineer</title><link>https://jobs.example.com/1</link><guid>1</guid><description>Go</description></item>
 <item><title>Platform Engineer</title><link>https://jobs.example.com/2</link><guid>2</guid></item>
 <item><link>https://jobs.example.com/3</link><guid>3</guid></item>
</channel></rss>`

func rssInput(name string) domain.SourceInput {
	return domain.SourceInput{Name: name, Kind: domain.KindRSS, Enabled: true, Config: domain.SourceConfig{URL: feedURL}}
}

func fileInput(doc string) domain.SourceInput {
	return domain.SourceInput{
		Name: "pasted", Kind: domain.KindFile, Enabled: true,
		Config: domain.SourceConfig{Document: doc, Mapping: &domain.FieldMapping{ID: "id", Title: "title", Company: "co"}},
	}
}

func count(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestRunningAnRSSSourceStoresItsPostingsOnceAndRecordsTheRun(t *testing.T) {
	f := &fakeFetcher{docs: map[string]string{feedURL: feedXML}}
	svc, pool := newService(t, f)
	owner := uuid.New()
	src, err := svc.UpsertSource(asOwner(owner), nil, rssInput("feed"))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	first, firstErr := svc.RunSource(asOwner(owner), src.ID)
	second, secondErr := svc.RunSource(asOwner(owner), src.ID)

	if firstErr != nil || first != (app.RunResult{Fetched: 3, Added: 2, Skipped: 1}) {
		t.Fatalf("first = %+v, %v; want 3 listed, 2 stored and the one without a title skipped", first, firstErr)
	}
	if secondErr != nil || second.Added != 0 || second.Fetched != 3 || count(t, pool, "postings") != 2 {
		t.Fatalf("second = %+v, %v, postings %d; want no duplicates", second, secondErr, count(t, pool, "postings"))
	}
	row, _ := store.New(pool).GetSource(context.Background(), owner, src.ID)
	if row.LastRunAt == nil || !row.LastRunAt.Equal(testNow) || row.LastError != nil {
		t.Fatalf("source = %+v, want the run recorded and no error", row)
	}
}

func TestRunningAFileSourceStoresItsPostingsOnceAndNeverFetches(t *testing.T) {
	f := &fakeFetcher{}
	svc, pool := newService(t, f)
	owner := uuid.New()
	src, err := svc.UpsertSource(asOwner(owner), nil, fileInput(`[{"id":"a","title":"Backend Engineer","co":"Acme"},{"id":"b","title":"Platform Engineer"}]`))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	first, _ := svc.RunSource(asOwner(owner), src.ID)
	second, err := svc.RunSource(asOwner(owner), src.ID)

	if first.Added != 2 || second.Added != 0 || err != nil || count(t, pool, "postings") != 2 || len(f.asked) != 0 {
		t.Fatalf("first %+v, second %+v, postings %d, fetches %d; want two postings, no duplicates and no fetch",
			first, second, count(t, pool, "postings"), len(f.asked))
	}
}

func TestRunningAnAPISourceReadsItsMappedItems(t *testing.T) {
	const apiURL = "https://jobs.example.com/api"
	f := &fakeFetcher{docs: map[string]string{apiURL: `{"data":[{"id":7,"title":"SRE","co":"Acme"}]}`}}
	svc, pool := newService(t, f)
	owner := uuid.New()
	src, _ := svc.UpsertSource(asOwner(owner), nil, domain.SourceInput{
		Name: "api", Kind: domain.KindAPI, Enabled: true,
		Config: domain.SourceConfig{URL: apiURL, Mapping: &domain.FieldMapping{ItemsPath: "data", ID: "id", Title: "title", Company: "co"}},
	})

	res, err := svc.RunSource(asOwner(owner), src.ID)

	if err != nil || res.Added != 1 || len(f.asked) != 1 || f.asked[0] != apiURL || count(t, pool, "postings") != 1 {
		t.Fatalf("res %+v, err %v, asked %v", res, err, f.asked)
	}
}

func TestAFailedRunIsRecordedWithAShortCodeAndStoresNothing(t *testing.T) {
	tests := []struct {
		name     string
		fetchErr error
		doc      string
		wantCode string
	}{
		{"robots", fetch.ErrRobotsDisallowed, "", app.CodeRobotsDisallowed},
		{"private address", fetch.ErrNotPublic, "", app.CodeAddressNotPublic},
		{"too large", fetch.ErrTooLarge, "", app.CodeTooLarge},
		{"unreachable", errors.Join(errors.New("dial"), fetch.ErrFailed), "", app.CodeFetchFailed},
		{"timeout", context.DeadlineExceeded, "", app.CodeFetchFailed},
		{"not a feed", nil, `{"json":true}`, app.CodeBadDocument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeFetcher{docs: map[string]string{feedURL: tt.doc}, err: tt.fetchErr}
			svc, pool := newService(t, f)
			owner := uuid.New()
			src, _ := svc.UpsertSource(asOwner(owner), nil, rssInput("feed"))

			_, err := svc.RunSource(asOwner(owner), src.ID)

			var runErr *app.RunError
			if !errors.As(err, &runErr) || runErr.Code != tt.wantCode {
				t.Fatalf("err = %v, want a RunError with code %s", err, tt.wantCode)
			}
			row, _ := store.New(pool).GetSource(context.Background(), owner, src.ID)
			if row.LastError == nil || *row.LastError != tt.wantCode || row.LastRunAt == nil || count(t, pool, "postings") != 0 {
				t.Fatalf("source = %+v, postings %d; want the failure kept and nothing stored", row, count(t, pool, "postings"))
			}
		})
	}
}

func TestASuccessfulRunClearsTheLastError(t *testing.T) {
	f := &fakeFetcher{err: fetch.ErrFailed}
	svc, pool := newService(t, f)
	owner := uuid.New()
	src, _ := svc.UpsertSource(asOwner(owner), nil, rssInput("feed"))
	_, _ = svc.RunSource(asOwner(owner), src.ID)
	f.err, f.docs = nil, map[string]string{feedURL: feedXML}

	_, err := svc.RunSource(asOwner(owner), src.ID)

	row, _ := store.New(pool).GetSource(context.Background(), owner, src.ID)
	if err != nil || row.LastError != nil {
		t.Fatalf("err %v, last error %v; want it cleared", err, row.LastError)
	}
}

func TestRunSourceIsPerOwner(t *testing.T) {
	svc, _ := newService(t, &fakeFetcher{docs: map[string]string{feedURL: feedXML}})
	owner := uuid.New()
	src, _ := svc.UpsertSource(asOwner(owner), nil, rssInput("feed"))

	_, foreign := svc.RunSource(asOwner(uuid.New()), src.ID)
	_, missing := svc.RunSource(asOwner(owner), uuid.New())
	_, noOwner := svc.RunSource(context.Background(), src.ID)

	if !errors.Is(foreign, app.ErrSourceNotFound) || !errors.Is(missing, app.ErrSourceNotFound) || !errors.Is(noOwner, app.ErrNoOwner) {
		t.Fatalf("foreign %v, missing %v, no owner %v", foreign, missing, noOwner)
	}
}

func TestUpsertSourceCreatesUpdatesAndKeepsTheKind(t *testing.T) {
	svc, _ := newService(t, &fakeFetcher{})
	owner := uuid.New()
	created, err := svc.UpsertSource(asOwner(owner), nil, rssInput("feed"))
	renamed := rssInput("renamed")
	renamed.Schedule = "30 6 * * *"
	updated, updateErr := svc.UpsertSource(asOwner(owner), &created.ID, renamed)
	_, kindErr := svc.UpsertSource(asOwner(owner), &created.ID, fileInput(`[]`))
	_, missing := svc.UpsertSource(asOwner(owner), ptr(uuid.New()), rssInput("x"))
	_, foreign := svc.UpsertSource(asOwner(uuid.New()), &created.ID, rssInput("x"))
	_, invalid := svc.UpsertSource(asOwner(owner), nil, domain.SourceInput{Name: "x", Kind: domain.KindRSS, Config: domain.SourceConfig{URL: "http://insecure.example"}})

	if err != nil || created.Schedule != domain.DefaultSchedule || created.Name != "feed" {
		t.Fatalf("created = %+v, %v", created, err)
	}
	if updateErr != nil || updated.ID != created.ID || updated.Name != "renamed" || updated.Schedule != "30 6 * * *" {
		t.Fatalf("updated = %+v, %v", updated, updateErr)
	}
	if !errors.Is(kindErr, app.ErrKindFixed) || !errors.Is(missing, app.ErrSourceNotFound) || !errors.Is(foreign, app.ErrSourceNotFound) || !errors.Is(invalid, domain.ErrInvalid) {
		t.Fatalf("kind %v, missing %v, foreign %v, invalid %v", kindErr, missing, foreign, invalid)
	}
}

func ptr[T any](v T) *T { return &v }

func TestAnOwnerCannotHaveMoreThanTheMostSources(t *testing.T) {
	svc, _ := newService(t, &fakeFetcher{})
	owner := uuid.New()
	for i := range app.MaxSources {
		if _, err := svc.UpsertSource(asOwner(owner), nil, rssInput(string(rune('a'+i)))); err != nil {
			t.Fatalf("source %d: %v", i, err)
		}
	}

	_, err := svc.UpsertSource(asOwner(owner), nil, rssInput("one too many"))
	_, other := svc.UpsertSource(asOwner(uuid.New()), nil, rssInput("fine"))

	if !errors.Is(err, app.ErrTooManySources) || other != nil {
		t.Fatalf("err %v, other owner %v", err, other)
	}
}

func TestListSourcesIsPerOwner(t *testing.T) {
	svc, _ := newService(t, &fakeFetcher{})
	owner := uuid.New()
	_, _ = svc.UpsertSource(asOwner(owner), nil, rssInput("mine"))
	_, _ = svc.UpsertSource(asOwner(uuid.New()), nil, rssInput("theirs"))

	rows, err := svc.ListSources(asOwner(owner))

	if err != nil || len(rows) != 1 || rows[0].Name != "mine" {
		t.Fatalf("rows %+v, %v", rows, err)
	}
}

func TestDueSourcesOffersEachDueEnabledSourceWithItsSlot(t *testing.T) {
	svc, pool := newService(t, &fakeFetcher{docs: map[string]string{feedURL: feedXML}})
	ist, _ := time.LoadLocation("Asia/Kolkata")
	repo := store.New(pool)
	owner := uuid.New()
	never, _ := svc.UpsertSource(asOwner(owner), nil, rssInput("never run"))
	ranToday, _ := svc.UpsertSource(asOwner(owner), nil, rssInput("ran today"))
	ranYesterday, _ := svc.UpsertSource(asOwner(owner), nil, rssInput("ran yesterday"))
	off := rssInput("disabled")
	off.Enabled = false
	_, _ = svc.UpsertSource(asOwner(owner), nil, off)
	// testNow is 09:30 IST. Seven o'clock today has passed; seven tomorrow has not.
	_ = repo.MarkSourceRun(context.Background(), ranToday.ID, time.Date(2026, 10, 7, 7, 5, 0, 0, ist), "")
	_ = repo.MarkSourceRun(context.Background(), ranYesterday.ID, time.Date(2026, 10, 6, 7, 5, 0, 0, ist), "")

	due, err := svc.DueSources(context.Background(), testNow, ist)

	bySource := map[uuid.UUID]app.DueRun{}
	for _, d := range due {
		bySource[d.SourceID] = d
	}
	if err != nil || len(due) != 2 {
		t.Fatalf("due = %+v, %v; want the never-run and yesterday's", due, err)
	}
	if bySource[never.ID].Slot != domain.FirstRunSlot || bySource[ranYesterday.ID].Slot != "2026-10-07T01:30:00Z" ||
		bySource[never.ID].OwnerID != owner {
		t.Fatalf("by source = %+v", bySource)
	}
	if _, ok := bySource[ranToday.ID]; ok {
		t.Fatal("a source that already ran today is not due")
	}
}
