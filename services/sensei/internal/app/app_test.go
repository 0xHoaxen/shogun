package app_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/sensei/internal/app"
	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store"
	"github.com/0xHoaxen/shogun/services/sensei/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

// testNow is 02:00 on 8 October 2026 in Asia/Kolkata, which is still 7 October in UTC.
var testNow = time.Date(2026, 10, 7, 20, 30, 0, 0, time.UTC)

func newService(t *testing.T) (*app.Service, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "sensei")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return app.NewService(pool, func() time.Time { return testNow }), pool
}

func asOwner(owner uuid.UUID) context.Context {
	return authz.WithIdentity(context.Background(), authz.Identity{OwnerID: owner.String(), RequestID: "r"})
}

// fact stores a fact at the given IST wall time.
func fact(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID, typ string, dim map[string]string, ist time.Time) {
	t.Helper()
	zone := time.FixedZone("IST", 5*3600+1800)
	at := time.Date(ist.Year(), ist.Month(), ist.Day(), ist.Hour(), ist.Minute(), 0, 0, zone)
	if _, err := store.New(pool).InsertFact(context.Background(), domain.Fact{EventID: store.NewID(), OwnerID: owner, Type: typ, Dimension: dim, OccurredAt: at}); err != nil {
		t.Fatalf("fact: %v", err)
	}
}

func istAt(m time.Month, d, h, mins int) time.Time {
	return time.Date(2026, m, d, h, mins, 0, 0, time.UTC)
}

type rollupRow struct {
	owner, metric, dim, day string
	value                   int64
}

func rollups(t *testing.T, pool *pgxpool.Pool) []rollupRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT owner_id::text, day::text, metric, dimension, value FROM daily_rollups ORDER BY owner_id, day, metric, dimension`)
	if err != nil {
		t.Fatalf("rollups: %v", err)
	}
	defer rows.Close()
	var out []rollupRow
	for rows.Next() {
		var r rollupRow
		if err := rows.Scan(&r.owner, &r.day, &r.metric, &r.dim, &r.value); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func seedJobs(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID) {
	t.Helper()
	fact(t, pool, owner, "job.added", map[string]string{"source": "linkedin", "job_id": "j1"}, istAt(10, 1, 9, 0))
	fact(t, pool, owner, "job.added", map[string]string{"source": "referral", "job_id": "j2"}, istAt(10, 1, 10, 0))
	fact(t, pool, owner, "job.status_changed", map[string]string{"job_id": "j1", "from": "saved", "to": "applied"}, istAt(10, 2, 9, 0))
	fact(t, pool, owner, "job.status_changed", map[string]string{"job_id": "j2", "from": "saved", "to": "applied"}, istAt(10, 2, 9, 5))
	fact(t, pool, owner, "job.status_changed", map[string]string{"job_id": "j1", "from": "applied", "to": "interview"}, istAt(10, 4, 9, 0))
	fact(t, pool, owner, "job.status_changed", map[string]string{"job_id": "j1", "from": "interview", "to": "offer"}, istAt(10, 6, 9, 0))
	fact(t, pool, owner, "job.status_changed", map[string]string{"job_id": "jX", "from": "saved", "to": "applied"}, istAt(10, 3, 9, 0)) // added before sensei
	fact(t, pool, owner, "job.status_changed", map[string]string{"job_id": "j2", "from": "applied", "to": "rejected"}, istAt(10, 5, 9, 0))
	fact(t, pool, owner, "job.status_changed", map[string]string{"job_id": "j2", "from": "saved", "to": "saved"}, istAt(10, 5, 9, 1)) // not a funnel stage
}

func TestRollupCountsJobsAndStagesBySourceAndDay(t *testing.T) {
	svc, pool := newService(t)
	owner := uuid.New()
	seedJobs(t, pool, owner)

	if err := svc.Rollup(context.Background()); err != nil {
		t.Fatalf("Rollup: %v", err)
	}

	got := map[string]int64{}
	for _, r := range rollups(t, pool) {
		got[r.day+" "+r.metric+" "+r.dim] += r.value
	}
	want := map[string]int64{
		"2026-10-01 jobs_added source=linkedin": 1, "2026-10-01 jobs_added source=referral": 1,
		"2026-10-02 applications source=linkedin": 1, "2026-10-02 applications source=referral": 1,
		"2026-10-03 applications source=unknown": 1,
		"2026-10-04 interviews source=linkedin":  1,
		"2026-10-05 rejections source=referral":  1,
		"2026-10-06 offers source=linkedin":      1,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d (all: %v)", k, got[k], v, got)
		}
	}
}

func TestRollupCountsContactMovesRepliesAndFirstContacts(t *testing.T) {
	svc, pool := newService(t)
	owner := uuid.New()
	move := func(to, channel string, d int) {
		fact(t, pool, owner, "contact.status_changed", map[string]string{"from": "x", "to": to, "channel": channel}, istAt(10, d, 9, 0))
	}
	move("reached_out", "email", 1)
	move("reached_out", "email", 2)
	move("reached_out", "linkedin", 2)
	move("replied", "email", 3)
	move("conversation_started", "email", 3)

	if err := svc.Rollup(context.Background()); err != nil {
		t.Fatalf("Rollup: %v", err)
	}

	got := map[string]int64{}
	for _, r := range rollups(t, pool) {
		got[r.metric+" "+r.dim] += r.value
	}
	want := map[string]int64{
		"outreach_sent channel=email": 2, "outreach_sent channel=linkedin": 1, "replies channel=email": 1,
		"contact_moves status=reached_out": 3, "contact_moves status=replied": 1, "contact_moves status=conversation_started": 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d (all: %v)", k, got[k], v, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRollupUsesTheIndianDayNotTheUTCDay(t *testing.T) {
	svc, pool := newService(t)
	owner := uuid.New()
	// 01:00 IST on 2 October is 19:30 UTC on 1 October.
	fact(t, pool, owner, "job.added", map[string]string{"source": "x", "job_id": "j"}, istAt(10, 2, 1, 0))

	if err := svc.Rollup(context.Background()); err != nil {
		t.Fatalf("Rollup: %v", err)
	}

	if rows := rollups(t, pool); len(rows) != 1 || rows[0].day != "2026-10-02" {
		t.Fatalf("rows = %+v, want the IST day 2026-10-02", rows)
	}
}

func TestRollupTwiceLeavesRollupsUnchangedAndDropsStaleRows(t *testing.T) {
	svc, pool := newService(t)
	owner := uuid.New()
	seedJobs(t, pool, owner)
	// A row for a day inside the window that no fact backs is not kept.
	if _, err := pool.Exec(context.Background(), `INSERT INTO daily_rollups VALUES ($1, '2026-10-03', 'jobs_added', 'source=ghost', 5)`, owner); err != nil {
		t.Fatal(err)
	}

	_ = svc.Rollup(context.Background())
	first := rollups(t, pool)
	_ = svc.Rollup(context.Background())
	second := rollups(t, pool)

	if len(first) == 0 || len(first) != len(second) {
		t.Fatalf("first %d rows, second %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("row %d changed: %+v then %+v", i, first[i], second[i])
		}
		if first[i].dim == "source=ghost" {
			t.Fatalf("stale row kept: %+v", first[i])
		}
	}
}

func TestRollupLeavesDaysOlderThanTheWindowAlone(t *testing.T) {
	svc, pool := newService(t)
	owner := uuid.New()
	old := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(context.Background(), `INSERT INTO daily_rollups VALUES ($1, $2, 'jobs_added', 'source=old', 3)`, owner, old); err != nil {
		t.Fatal(err)
	}

	if err := svc.Rollup(context.Background()); err != nil {
		t.Fatalf("Rollup: %v", err)
	}

	rows := rollups(t, pool)
	if len(rows) != 1 || rows[0].dim != "source=old" || rows[0].value != 3 {
		t.Fatalf("rows = %+v, want the old row kept", rows)
	}
}

func TestRollupKeepsOwnersApart(t *testing.T) {
	svc, pool := newService(t)
	a, b := uuid.New(), uuid.New()
	fact(t, pool, a, "job.added", map[string]string{"source": "x", "job_id": "j"}, istAt(10, 1, 9, 0))
	fact(t, pool, b, "job.added", map[string]string{"source": "x", "job_id": "j"}, istAt(10, 1, 9, 0))
	fact(t, pool, b, "job.added", map[string]string{"source": "x", "job_id": "k"}, istAt(10, 1, 9, 0))
	fact(t, pool, b, "job.status_changed", map[string]string{"job_id": "j", "to": "applied"}, istAt(10, 2, 9, 0))

	_ = svc.Rollup(context.Background())

	counts := map[string]int64{}
	for _, r := range rollups(t, pool) {
		counts[r.owner+r.metric] += r.value
	}
	if counts[a.String()+"jobs_added"] != 1 || counts[b.String()+"jobs_added"] != 2 || counts[a.String()+"applications"] != 0 || counts[b.String()+"applications"] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestGetFunnelReadsTheRollupsOfTheCallingOwner(t *testing.T) {
	svc, pool := newService(t)
	owner := uuid.New()
	seedJobs(t, pool, owner)
	fact(t, pool, uuid.New(), "job.added", map[string]string{"source": "linkedin", "job_id": "other"}, istAt(10, 1, 9, 0))
	_ = svc.Rollup(context.Background())

	res, err := svc.GetFunnel(asOwner(owner), "2026-10-01", "2026-10-07", domain.FunnelBySource)

	if err != nil || res.From.Format(time.DateOnly) != "2026-10-01" || res.To.Format(time.DateOnly) != "2026-10-07" {
		t.Fatalf("res %+v, err %v", res, err)
	}
	if res.Total.JobsAdded != 2 || res.Total.Applications != 3 || res.Total.Interviews != 1 || res.Total.Offers != 1 || res.Total.Rejections != 1 {
		t.Fatalf("total = %+v", res.Total)
	}
	if len(res.Rows) != 3 || res.Rows[0].Key != "linkedin" || res.Rows[0].Applications != 1 || res.Rows[0].InterviewRate() != 1 {
		t.Fatalf("rows = %+v", res.Rows)
	}
	narrow, _ := svc.GetFunnel(asOwner(owner), "2026-10-04", "2026-10-04", domain.FunnelByMonth)
	if len(narrow.Rows) != 1 || narrow.Rows[0].Key != "2026-10" || narrow.Total.Interviews != 1 || narrow.Total.Applications != 0 {
		t.Fatalf("narrow = %+v", narrow)
	}
}

func TestGetFunnelDefaultsToTheLastThirtyDaysEndingToday(t *testing.T) {
	svc, pool := newService(t)
	owner := uuid.New()
	fact(t, pool, owner, "job.added", map[string]string{"source": "x", "job_id": "j"}, istAt(9, 8, 9, 0)) // day 30 of the window
	fact(t, pool, owner, "job.added", map[string]string{"source": "x", "job_id": "k"}, istAt(9, 7, 9, 0)) // one day too old
	_ = svc.Rollup(context.Background())

	res, err := svc.GetFunnel(asOwner(owner), "", "", domain.FunnelBySource)

	// testNow is 8 October in India.
	if err != nil || res.To.Format(time.DateOnly) != "2026-10-08" || res.From.Format(time.DateOnly) != "2026-09-09" {
		t.Fatalf("range %v..%v, err %v", res.From, res.To, err)
	}
	if res.Total.JobsAdded != 0 {
		t.Fatalf("total = %+v; 8 September is outside 9 September..8 October", res.Total)
	}
}

func TestGetOutreachStatsReadsTheRollups(t *testing.T) {
	svc, pool := newService(t)
	owner := uuid.New()
	for i := range 4 {
		fact(t, pool, owner, "contact.status_changed", map[string]string{"to": "reached_out", "channel": "email"}, istAt(10, 1+i, 9, 0))
	}
	fact(t, pool, owner, "contact.status_changed", map[string]string{"to": "replied", "channel": "email"}, istAt(10, 6, 9, 0))
	_ = svc.Rollup(context.Background())

	byChannel, err := svc.GetOutreachStats(asOwner(owner), "2026-10-01", "2026-10-07", domain.OutreachByChannel)
	byStatus, _ := svc.GetOutreachStats(asOwner(owner), "2026-10-01", "2026-10-07", domain.OutreachByStatus)

	if err != nil || len(byChannel.Rows) != 1 || byChannel.Rows[0].Sent != 4 || byChannel.Rows[0].Replied != 1 || byChannel.Rows[0].ReplyRate() != 0.25 {
		t.Fatalf("by channel = %+v, %v", byChannel, err)
	}
	if len(byStatus.Rows) != 2 || byStatus.Rows[0].Key != "reached_out" || byStatus.Rows[0].MovedIn != 4 || byStatus.Total.ReplyRate() != 0.25 {
		t.Fatalf("by status = %+v", byStatus)
	}
}

func TestStatsRefuseABadRangeAndACallWithoutAnOwner(t *testing.T) {
	svc, _ := newService(t)

	_, badRange := svc.GetFunnel(asOwner(uuid.New()), "2026-10-09", "2026-10-01", domain.FunnelBySource)
	_, badOutreach := svc.GetOutreachStats(asOwner(uuid.New()), "soon", "", domain.OutreachByChannel)
	_, noOwner := svc.GetFunnel(context.Background(), "", "", domain.FunnelBySource)

	if !errors.Is(badRange, domain.ErrInvalidRange) || !errors.Is(badOutreach, domain.ErrInvalidRange) || !errors.Is(noOwner, app.ErrNoOwner) {
		t.Fatalf("errors: %v | %v | %v", badRange, badOutreach, noOwner)
	}
}
