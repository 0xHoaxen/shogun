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
	"github.com/0xHoaxen/shogun/pkg/owners"
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

// digestDate is digestDay as the job carries it.
const digestDate = "2026-10-07"

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
		digester: app.NewDigester(svc, sources, clock, slog.New(slog.DiscardHandler)),
		sources:  sources, pool: pool, repo: store.New(pool),
	}
}

// owner makes taiko know a new active owner in India time, as torii's
// user.activated would.
func (e *digestEnv) owner(t *testing.T) uuid.UUID {
	t.Helper()
	return e.ownerIn(t, "Asia/Kolkata", owners.StatusActive)
}

// ownerIn makes taiko know a new owner with the given zone and status.
func (e *digestEnv) ownerIn(t *testing.T, timezone, status string) uuid.UUID {
	t.Helper()
	owner := uuid.New()
	if err := owners.Upsert(context.Background(), e.pool, owners.Owner{ID: owner, Timezone: timezone, Status: status, Version: 1}); err != nil {
		t.Fatalf("upsert owner: %v", err)
	}
	return owner
}

// save writes a setting over whatever version is stored, as a test setup step.
func (e *digestEnv) save(t *testing.T, in store.ChannelSettingInput) {
	t.Helper()
	ctx := context.Background()
	current, err := e.repo.ChannelSetting(ctx, in.OwnerID, in.Channel)
	switch {
	case err == nil:
		in.Version = current.Version
	case !errors.Is(err, store.ErrNotFound):
		t.Fatalf("read setting: %v", err)
	}
	if _, err := e.repo.SaveChannelSetting(ctx, in); err != nil {
		t.Fatalf("save setting: %v", err)
	}
}

func (e *digestEnv) setInApp(t *testing.T, owner uuid.UUID, enabled bool, from, to time.Duration) {
	t.Helper()
	p := store.ChannelSettingInput{OwnerID: owner, Channel: store.ChannelInApp, Enabled: enabled}
	if from != to {
		p.QuietFrom = pgtype.Time{Microseconds: from.Microseconds(), Valid: true}
		p.QuietTo = pgtype.Time{Microseconds: to.Microseconds(), Valid: true}
	}
	e.save(t, p)
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

	wait, err := e.digester.RunFor(context.Background(), owner, digestDate)

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

	wait, err := e.digester.RunFor(context.Background(), owner, digestDate)

	if err != nil || wait != 0 || len(e.digests(t, owner)) != 0 {
		t.Fatalf("wait %s, err %v, digests %d; want none", wait, err, len(e.digests(t, owner)))
	}
}

func TestDigestRunTwiceForTheSameDateStoresOnce(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)
	e.sources.followUps = 1

	_, first := e.digester.RunFor(context.Background(), owner, digestDate)
	_, again := e.digester.RunFor(context.Background(), owner, digestDate)

	if first != nil || again != nil || len(e.digests(t, owner)) != 1 {
		t.Fatalf("errs %v %v, digests %d; want one", first, again, len(e.digests(t, owner)))
	}
}

func TestDigestOnAnotherDayIsAnotherDigest(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)
	e.sources.followUps = 1
	_, _ = e.digester.RunFor(context.Background(), owner, digestDate)
	next := app.NewDigester(newServiceOn(t, e.pool), e.sources,
		func() time.Time { return atDigestTime.AddDate(0, 0, 1) }, slog.New(slog.DiscardHandler))

	_, err := next.RunFor(context.Background(), owner, "2026-10-08")

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

	_, err := e.digester.RunFor(context.Background(), owner, digestDate)
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

			wait, err := e.digester.RunFor(context.Background(), owner, digestDate)

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
	wait, _ := held.digester.RunFor(context.Background(), owner, digestDate)
	later := app.NewDigester(newServiceOn(t, held.pool), held.sources,
		func() time.Time { return atDigestTime.Add(wait) }, slog.New(slog.DiscardHandler))

	rerun, err := later.RunFor(context.Background(), owner, digestDate)

	if err != nil || rerun != 0 || len(held.digests(t, owner)) != 1 {
		t.Fatalf("wait %s, err %v, digests %d; want one after the window", rerun, err, len(held.digests(t, owner)))
	}
}

func TestADisabledInAppChannelGetsNoDigest(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)
	e.sources.followUps = 5
	e.setInApp(t, owner, false, 0, 0)

	wait, err := e.digester.RunFor(context.Background(), owner, digestDate)

	if err != nil || wait != 0 || len(e.digests(t, owner)) != 0 || len(e.sources.owners) != 0 {
		t.Fatalf("wait %s, err %v, digests %d, calls %d; want nothing", wait, err, len(e.digests(t, owner)), len(e.sources.owners))
	}
}

func TestADigestForADayThatIsOverIsSkipped(t *testing.T) {
	e := newDigestEnv(t, digestDay.AddDate(0, 0, 1).Add(5*time.Minute))
	owner := e.owner(t)
	e.sources.followUps = 1

	wait, err := e.digester.RunFor(context.Background(), owner, digestDate)

	if err != nil || wait != 0 || len(e.digests(t, owner)) != 0 {
		t.Fatalf("wait %s, err %v, digests %d; want a stale digest dropped", wait, err, len(e.digests(t, owner)))
	}
}

func TestOwnersGetTheirOwnDigestEachInTheirOwnZone(t *testing.T) {
	// Arrange: at 03:00 UTC it is 08:30 in Kolkata and 12:00 in Tokyo.
	at := time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC)
	e := newDigestEnv(t, at)
	kolkata := e.ownerIn(t, "Asia/Kolkata", owners.StatusActive)
	tokyo := e.ownerIn(t, "Asia/Tokyo", owners.StatusActive)
	e.sources.followUps = 1

	// Act
	_, errKolkata := e.digester.RunFor(context.Background(), kolkata, digestDate)
	_, errTokyo := e.digester.RunFor(context.Background(), tokyo, digestDate)

	// Assert
	if errKolkata != nil || errTokyo != nil || len(e.digests(t, kolkata)) != 1 || len(e.digests(t, tokyo)) != 1 {
		t.Fatalf("errs %v %v, digests %d and %d; want one each", errKolkata, errTokyo, len(e.digests(t, kolkata)), len(e.digests(t, tokyo)))
	}
	tokyoDay := time.Date(2026, 10, 7, 0, 0, 0, 0, time.FixedZone("JST", 9*3600))
	if !e.sources.spendFrom.Equal(tokyoDay) {
		t.Fatalf("the last spend window starts %s, want the start of the Tokyo day %s", e.sources.spendFrom, tokyoDay)
	}
}

func TestADayThatIsOverInTheOwnersZoneIsSkippedEvenIfItIsStillTodayElsewhere(t *testing.T) {
	// Arrange: at 20:00 UTC on the 7th it is already the 8th in Tokyo.
	e := newDigestEnv(t, time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC))
	tokyo := e.ownerIn(t, "Asia/Tokyo", owners.StatusActive)
	e.sources.followUps = 1

	// Act
	wait, err := e.digester.RunFor(context.Background(), tokyo, digestDate)

	// Assert
	if err != nil || wait != 0 || len(e.digests(t, tokyo)) != 0 {
		t.Fatalf("wait %s, err %v, digests %d; want the 7th dropped in Tokyo", wait, err, len(e.digests(t, tokyo)))
	}
}

func TestAnOwnerTheServiceDoesNotKnowOrWhoIsNotActiveGetsNoDigest(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	e.sources.followUps = 1
	tests := map[string]uuid.UUID{
		"unknown":  uuid.New(),
		"disabled": e.ownerIn(t, "Asia/Kolkata", owners.StatusDisabled),
		"deleted":  e.ownerIn(t, "Asia/Kolkata", owners.StatusDeleted),
	}
	for name, owner := range tests {
		t.Run(name, func(t *testing.T) {
			wait, err := e.digester.RunFor(context.Background(), owner, digestDate)

			if err != nil || wait != 0 || len(e.digests(t, owner)) != 0 || len(e.sources.owners) != 0 {
				t.Fatalf("wait %s, err %v, digests %d, calls %d; want nothing", wait, err, len(e.digests(t, owner)), len(e.sources.owners))
			}
		})
	}
}

func TestAFailingSourceIsReportedSoTheJobRetries(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)
	boom := errors.New("kagami is down")
	e.sources.failFor = map[uuid.UUID]error{owner: boom}

	_, err := e.digester.RunFor(context.Background(), owner, digestDate)

	if !errors.Is(err, boom) || len(e.digests(t, owner)) != 0 {
		t.Fatalf("err %v, digests %d; want the failure reported and no digest", err, len(e.digests(t, owner)))
	}
}

func TestADateThatIsNotADateIsRefused(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)

	_, err := e.digester.RunFor(context.Background(), owner, "07/10/2026")

	if !errors.Is(err, app.ErrBadDigestDate) {
		t.Fatalf("err = %v, want ErrBadDigestDate", err)
	}
}
