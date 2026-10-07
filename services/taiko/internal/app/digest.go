package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/taiko/internal/domain"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
)

// maxDigestDrafts is how many waiting drafts the digest counts. The queue API
// has no total, so a longer queue is shown as "N+".
const maxDigestDrafts = 200

// digestNamespace derives the event id of a digest, so one owner gets one
// digest per date however often the job runs.
var digestNamespace = uuid.MustParse("b6f1c0de-7a54-5d1e-9a63-3c1d6f2e8a40")

// DigestSources reads what the digest summarises from the other services. Each
// call runs as the owner in ctx.
type DigestSources interface {
	// FollowUpsDue counts the jobs and contacts due on or before date.
	FollowUpsDue(ctx context.Context, date time.Time) (int, error)
	// DraftsWaiting counts drafts waiting for the owner, up to limit; capped
	// says there are more.
	DraftsWaiting(ctx context.Context, limit int) (count int, capped bool, err error)
	// Spend totals Claude spend from start up to end, in micro-dollars.
	Spend(ctx context.Context, start, end time.Time) (int64, error)
}

// Digester writes the daily digest notification.
type Digester struct {
	svc     *Service
	sources DigestSources
	loc     *time.Location
	now     func() time.Time
	log     *slog.Logger
}

// NewDigester returns a Digester. loc is the zone the owner's days are counted
// in and now is the clock; a nil now means time.Now.
func NewDigester(svc *Service, sources DigestSources, loc *time.Location, now func() time.Time, log *slog.Logger) *Digester {
	if now == nil {
		now = time.Now
	}
	return &Digester{svc: svc, sources: sources, loc: loc, now: now, log: log}
}

// Run writes the digest of date (a day in the owner's zone) for every owner.
// It returns how long to wait before running again when an owner's quiet hours
// hold the digest back, and zero otherwise. A digest that is empty, switched
// off, or for a day that is over is skipped. Running it again for the same date
// never adds a second digest. When some owners fail, the others are still done
// and the failures are returned together, so a retry fills the gaps.
func (d *Digester) Run(ctx context.Context, date time.Time) (time.Duration, error) {
	now := d.now().In(d.loc)
	if !sameDay(now, date) {
		d.log.Warn("daily digest skipped: its day is over", slog.String("date", date.Format(time.DateOnly)))
		return 0, nil
	}
	owners, err := store.New(d.svc.pool).Owners(ctx)
	if err != nil {
		return 0, err
	}
	var wait time.Duration
	var errs []error
	for _, owner := range owners {
		held, err := d.digestFor(ctx, owner, date, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("owner %s: %w", owner, err))
		}
		wait = max(wait, held)
	}
	if err := errors.Join(errs...); err != nil {
		return 0, err
	}
	return wait, nil
}

// digestFor writes one owner's digest, or returns how long their quiet hours
// hold it back.
func (d *Digester) digestFor(ctx context.Context, owner uuid.UUID, date, now time.Time) (time.Duration, error) {
	repo := store.New(d.svc.pool)
	setting, err := repo.ChannelSetting(ctx, owner, store.ChannelInApp)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// Never changed: the defaults are on, with no quiet hours.
	case err != nil:
		return 0, err
	case !setting.Enabled:
		return 0, nil
	case setting.QuietFrom.Valid && setting.QuietTo.Valid:
		quiet := domain.QuietHours{
			From: time.Duration(setting.QuietFrom.Microseconds) * time.Microsecond,
			To:   time.Duration(setting.QuietTo.Microseconds) * time.Microsecond,
		}
		if wait := quiet.Until(domain.TimeOfDay(now)); wait > 0 {
			return wait, nil
		}
	}

	digest, err := d.collect(authz.WithIdentity(ctx, authz.Identity{
		OwnerID: owner.String(), RequestID: "daily-digest-" + date.Format(time.DateOnly),
	}), date)
	if err != nil {
		return 0, err
	}
	if digest.Empty() {
		return 0, nil
	}
	notice, err := digest.Notice()
	if err != nil {
		return 0, err
	}
	eventID := uuid.NewSHA1(digestNamespace, []byte(owner.String()+"/"+date.Format(time.DateOnly)))
	return 0, d.record(ctx, RecordInput{OwnerID: owner, EventID: eventID, Notice: notice})
}

// collect reads the three parts of the digest as the owner in ctx.
func (d *Digester) collect(ctx context.Context, date time.Time) (domain.Digest, error) {
	followUps, err := d.sources.FollowUpsDue(ctx, date)
	if err != nil {
		return domain.Digest{}, fmt.Errorf("follow-ups: %w", err)
	}
	drafts, capped, err := d.sources.DraftsWaiting(ctx, maxDigestDrafts)
	if err != nil {
		return domain.Digest{}, fmt.Errorf("drafts: %w", err)
	}
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, d.loc)
	spend, err := d.sources.Spend(ctx, start, start.AddDate(0, 0, 1))
	if err != nil {
		return domain.Digest{}, fmt.Errorf("spend: %w", err)
	}
	return domain.Digest{FollowUps: followUps, Drafts: drafts, DraftsCapped: capped, SpendMicros: spend}, nil
}

// record stores the digest in its own transaction and announces it.
func (d *Digester) record(ctx context.Context, in RecordInput) error {
	tx, err := d.svc.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := d.svc.Record(ctx, tx, in); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
