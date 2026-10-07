package app_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

var (
	ist       = time.FixedZone("IST", 5*3600+1800)
	digestDay = time.Date(2026, 10, 7, 0, 0, 0, 0, ist)
	// atDigestTime is when the digest job fires: 08:30 on the digest's day.
	atDigestTime = time.Date(2026, 10, 7, 8, 30, 0, 0, ist)
)

// fakeSources answers from the fields a test sets and records how it was asked.
type fakeSources struct {
	mu        sync.Mutex
	followUps int
	drafts    int
	capped    bool
	spend     int64
	failFor   map[uuid.UUID]error
	owners    []uuid.UUID
	followDay time.Time
	spendFrom time.Time
	spendTo   time.Time
}

func (f *fakeSources) see(ctx context.Context) (uuid.UUID, error) {
	id, _ := authz.FromContext(ctx)
	owner, _ := uuid.Parse(id.OwnerID)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners = append(f.owners, owner)
	return owner, f.failFor[owner]
}

func (f *fakeSources) FollowUpsDue(ctx context.Context, date time.Time) (int, error) {
	if _, err := f.see(ctx); err != nil {
		return 0, err
	}
	f.followDay = date
	return f.followUps, nil
}

func (f *fakeSources) DraftsWaiting(context.Context, int) (int, bool, error) {
	return f.drafts, f.capped, nil
}

func (f *fakeSources) Spend(_ context.Context, start, end time.Time) (int64, error) {
	f.spendFrom, f.spendTo = start, end
	return f.spend, nil
}

type digestEnv struct {
	digester *app.Digester
	sources  *fakeSources
	pool     *pgxpool.Pool
	repo     *store.Repo
}

func newDigestEnv(t *testing.T, at time.Time) *digestEnv {
	t.Helper()
	svc, pool := newService(t)
	sources := &fakeSources{}
	clock := func() time.Time { return at }
	return &digestEnv{
		digester: app.NewDigester(svc, sources, ist, clock, slog.New(slog.DiscardHandler)),
		sources:  sources, pool: pool, repo: store.New(pool),
	}
}

// owner makes taiko know a new owner, as a first notification would.
func (e *digestEnv) owner(t *testing.T) uuid.UUID {
	t.Helper()
	owner := uuid.New()
	if _, err := e.repo.SaveChannelSetting(context.Background(), db.UpsertChannelSettingParams{
		OwnerID: owner, Channel: store.ChannelEmailDigest, Enabled: true,
	}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	return owner
}

func (e *digestEnv) setInApp(t *testing.T, owner uuid.UUID, enabled bool, from, to time.Duration) {
	t.Helper()
	p := db.UpsertChannelSettingParams{OwnerID: owner, Channel: store.ChannelInApp, Enabled: enabled}
	if from != to {
		p.QuietFrom = pgtype.Time{Microseconds: from.Microseconds(), Valid: true}
		p.QuietTo = pgtype.Time{Microseconds: to.Microseconds(), Valid: true}
	}
	if _, err := e.repo.SaveChannelSetting(context.Background(), p); err != nil {
		t.Fatalf("save setting: %v", err)
	}
}

func (e *digestEnv) digests(t *testing.T, owner uuid.UUID) []db.Notification {
	t.Helper()
	rows, _, err := e.repo.List(context.Background(), owner, false, store.Page{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var out []db.Notification
	for _, n := range rows {
		if n.Type == "daily_digest" {
			out = append(out, n)
		}
	}
	return out
}

func hours(h, m int) time.Duration { return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute }

func TestDigestWritesOneNotificationWithTheCounts(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)
	e.sources.followUps, e.sources.drafts, e.sources.spend = 2, 3, 6_400_000

	wait, err := e.digester.Run(context.Background(), digestDay)

	got := e.digests(t, owner)
	if err != nil || wait != 0 || len(got) != 1 {
		t.Fatalf("wait %s, err %v, %d digests", wait, err, len(got))
	}
	if *got[0].Body != "2 follow-ups due, 3 drafts waiting, $6.40 spent." || got[0].Title != "Daily digest" || *got[0].Link != "/drafts" {
		t.Fatalf("got %+v", got[0])
	}
}

func TestDigestOfNothingIsSkipped(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)

	wait, err := e.digester.Run(context.Background(), digestDay)

	if err != nil || wait != 0 || len(e.digests(t, owner)) != 0 {
		t.Fatalf("wait %s, err %v, digests %d; want none", wait, err, len(e.digests(t, owner)))
	}
}

func TestDigestRunTwiceForTheSameDateStoresOnce(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)
	e.sources.followUps = 1

	_, first := e.digester.Run(context.Background(), digestDay)
	_, again := e.digester.Run(context.Background(), digestDay)

	if first != nil || again != nil || len(e.digests(t, owner)) != 1 {
		t.Fatalf("errs %v %v, digests %d; want one", first, again, len(e.digests(t, owner)))
	}
}

func TestDigestOnAnotherDayIsAnotherDigest(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)
	e.sources.followUps = 1
	_, _ = e.digester.Run(context.Background(), digestDay)
	next := app.NewDigester(newServiceOn(t, e.pool), e.sources, ist,
		func() time.Time { return atDigestTime.AddDate(0, 0, 1) }, slog.New(slog.DiscardHandler))

	_, err := next.Run(context.Background(), digestDay.AddDate(0, 0, 1))

	if err != nil || len(e.digests(t, owner)) != 2 {
		t.Fatalf("err %v, digests %d; want one per day", err, len(e.digests(t, owner)))
	}
}

func newServiceOn(t *testing.T, pool *pgxpool.Pool) *app.Service {
	t.Helper()
	return app.NewService(pool, func() time.Time { return atDigestTime })
}

func TestDigestAsksForTheOwnersDayInTheOwnersZone(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)
	e.sources.followUps = 1

	_, err := e.digester.Run(context.Background(), digestDay)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(e.sources.owners) != 1 || e.sources.owners[0] != owner {
		t.Fatalf("sources were asked as %v, want %s", e.sources.owners, owner)
	}
	if e.sources.followDay.Format(time.DateOnly) != "2026-10-07" {
		t.Fatalf("follow-ups asked for %s", e.sources.followDay)
	}
	if !e.sources.spendFrom.Equal(digestDay) || !e.sources.spendTo.Equal(digestDay.AddDate(0, 0, 1)) {
		t.Fatalf("spend asked for %s to %s, want the IST day", e.sources.spendFrom, e.sources.spendTo)
	}
}

func TestQuietHoursHoldTheDigestBackAndSayForHowLong(t *testing.T) {
	tests := []struct {
		name     string
		from, to time.Duration
		wantWait time.Duration
	}{
		{"window around the digest time", hours(8, 0), hours(9, 0), 30 * time.Minute},
		{"overnight window that ends later", hours(22, 0), hours(10, 0), 90 * time.Minute},
		{"window wrapping past midnight, ending at the digest time", hours(22, 0), hours(8, 30), 0},
		{"window that has ended", hours(22, 0), hours(8, 0), 0},
		{"window not started yet", hours(12, 0), hours(14, 0), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newDigestEnv(t, atDigestTime)
			owner := e.owner(t)
			e.sources.followUps = 1
			e.setInApp(t, owner, true, tt.from, tt.to)

			wait, err := e.digester.Run(context.Background(), digestDay)

			held := tt.wantWait > 0
			if err != nil || wait != tt.wantWait {
				t.Fatalf("wait %s, err %v; want wait %s", wait, err, tt.wantWait)
			}
			if got := len(e.digests(t, owner)); (got == 0) != held {
				t.Fatalf("%d digests, held back %v", got, held)
			}
			if held && len(e.sources.owners) != 0 {
				t.Fatal("the other services were called for a digest held back")
			}
		})
	}
}

func TestADigestHeldBackIsWrittenOnceQuietHoursEnd(t *testing.T) {
	held := newDigestEnv(t, atDigestTime)
	owner := held.owner(t)
	held.sources.followUps = 1
	held.setInApp(t, owner, true, hours(8, 0), hours(9, 0))
	wait, _ := held.digester.Run(context.Background(), digestDay)
	later := app.NewDigester(newServiceOn(t, held.pool), held.sources, ist,
		func() time.Time { return atDigestTime.Add(wait) }, slog.New(slog.DiscardHandler))

	rerun, err := later.Run(context.Background(), digestDay)

	if err != nil || rerun != 0 || len(held.digests(t, owner)) != 1 {
		t.Fatalf("wait %s, err %v, digests %d; want one after the window", rerun, err, len(held.digests(t, owner)))
	}
}

func TestADisabledInAppChannelGetsNoDigest(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)
	e.sources.followUps = 5
	e.setInApp(t, owner, false, 0, 0)

	wait, err := e.digester.Run(context.Background(), digestDay)

	if err != nil || wait != 0 || len(e.digests(t, owner)) != 0 || len(e.sources.owners) != 0 {
		t.Fatalf("wait %s, err %v, digests %d, calls %d; want nothing", wait, err, len(e.digests(t, owner)), len(e.sources.owners))
	}
}

func TestADigestForADayThatIsOverIsSkipped(t *testing.T) {
	e := newDigestEnv(t, digestDay.AddDate(0, 0, 1).Add(5*time.Minute))
	owner := e.owner(t)
	e.sources.followUps = 1

	wait, err := e.digester.Run(context.Background(), digestDay)

	if err != nil || wait != 0 || len(e.digests(t, owner)) != 0 {
		t.Fatalf("wait %s, err %v, digests %d; want a stale digest dropped", wait, err, len(e.digests(t, owner)))
	}
}

func TestEachOwnerGetsTheirOwnDigest(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	first, second := e.owner(t), e.owner(t)
	e.sources.followUps = 1

	_, err := e.digester.Run(context.Background(), digestDay)

	if err != nil || len(e.digests(t, first)) != 1 || len(e.digests(t, second)) != 1 {
		t.Fatalf("err %v, digests %d and %d; want one each", err, len(e.digests(t, first)), len(e.digests(t, second)))
	}
}

func TestOneOwnersFailureDoesNotStopTheOthersAndIsReported(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	broken, fine := e.owner(t), e.owner(t)
	boom := errors.New("kagami is down")
	e.sources.failFor = map[uuid.UUID]error{broken: boom}
	e.sources.followUps = 1

	_, err := e.digester.Run(context.Background(), digestDay)

	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the failure reported so the job retries", err)
	}
	if len(e.digests(t, fine)) != 1 || len(e.digests(t, broken)) != 0 {
		t.Fatalf("digests %d for the working owner and %d for the broken one; want 1 and 0", len(e.digests(t, fine)), len(e.digests(t, broken)))
	}
}

func TestDigestWithNoKnownOwnersDoesNothing(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)

	wait, err := e.digester.Run(context.Background(), digestDay)

	if err != nil || wait != 0 || len(e.sources.owners) != 0 {
		t.Fatalf("wait %s, err %v, calls %d", wait, err, len(e.sources.owners))
	}
}
