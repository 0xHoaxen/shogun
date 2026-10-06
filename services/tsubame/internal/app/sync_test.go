package app_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail/mailtest"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

type fakeQueue struct {
	mu  sync.Mutex
	ids []uuid.UUID
	err error
}

func (q *fakeQueue) EnqueueClassify(_ context.Context, _ pgx.Tx, _, messageID uuid.UUID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.ids = append(q.ids, messageID)
	return nil
}

type syncEnv struct {
	pool    *pgxpool.Pool
	syncer  *app.Syncer
	fake    *mailtest.Fake
	queue   *fakeQueue
	account db.Account
	now     time.Time
}

func newSyncEnv(t *testing.T) *syncEnv {
	t.Helper()
	accounts, pool := newAccounts(t)
	owner := uuid.New()
	acc, err := accounts.Connect(context.Background(), owner, "gmail", "me@example.com", refresh)
	if err != nil {
		t.Fatal(err)
	}
	fake := &mailtest.Fake{Address: "me@example.com"}
	queue := &fakeQueue{}
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	return &syncEnv{
		pool: pool, fake: fake, queue: queue, account: acc, now: now,
		syncer: app.NewSyncer(pool, accounts, fake, queue, slog.New(slog.DiscardHandler), func() time.Time { return now }),
	}
}

func (e *syncEnv) add(n int, direction mail.Direction) {
	for i := range n {
		id := fmt.Sprintf("m%d", len(e.fake.Messages)+1)
		e.fake.Messages = append(e.fake.Messages, mail.Message{
			ID: id, ThreadID: "t-" + id, Direction: direction, From: "hr@lumen.example", To: []string{"me@example.com"},
			Subject: "Subject " + id, Snippet: "snippet", ReceivedAt: e.now.Add(time.Duration(i) * time.Minute),
		})
	}
}

func (e *syncEnv) account0(t *testing.T) db.Account {
	t.Helper()
	acc, err := store.New(e.pool).GetAccount(context.Background(), e.account.OwnerID, e.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	return acc
}

func (e *syncEnv) messages(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func cursorOf(a db.Account) string {
	if a.HistoryID == nil {
		return ""
	}
	return *a.HistoryID
}

func TestFirstSyncReadsTheLastMonthAndStartsTheCursorAtTheProfile(t *testing.T) {
	e := newSyncEnv(t)
	e.add(3, mail.Inbound)

	res, err := e.syncer.SyncAccount(context.Background(), e.account)

	acc := e.account0(t)
	if err != nil || res.Added != 3 || !res.Full || cursorOf(acc) != "3" || acc.LastSyncedAt == nil || !acc.LastSyncedAt.Equal(e.now) {
		t.Fatalf("res %+v, err %v, cursor %q", res, err, cursorOf(acc))
	}
	if e.messages(t) != 3 || len(e.queue.ids) != 3 {
		t.Fatalf("stored %d, queued %d; want 3 each", e.messages(t), len(e.queue.ids))
	}
}

func TestLaterSyncsReadOnlyWhatIsNewAndNeverTwice(t *testing.T) {
	e := newSyncEnv(t)
	e.add(2, mail.Inbound)
	if _, err := e.syncer.SyncAccount(context.Background(), e.account); err != nil {
		t.Fatal(err)
	}
	e.add(2, mail.Inbound)

	res, err := e.syncer.SyncAccount(context.Background(), e.account0(t))
	again, againErr := e.syncer.SyncAccount(context.Background(), e.account0(t))

	if err != nil || res.Added != 2 || res.Full || cursorOf(e.account0(t)) != "4" {
		t.Fatalf("res %+v, err %v, cursor %q", res, err, cursorOf(e.account0(t)))
	}
	if againErr != nil || again.Added != 0 || e.messages(t) != 4 || len(e.queue.ids) != 4 {
		t.Fatalf("again %+v, %v; stored %d, queued %d", again, againErr, e.messages(t), len(e.queue.ids))
	}
}

func TestAnExpiredCursorFallsBackToAFullSyncWithoutDuplicates(t *testing.T) {
	e := newSyncEnv(t)
	e.add(2, mail.Inbound)
	if _, err := e.syncer.SyncAccount(context.Background(), e.account); err != nil {
		t.Fatal(err)
	}
	e.add(1, mail.Inbound)
	e.fake.ExpiredBefore = 99 // every cursor is too old

	res, err := e.syncer.SyncAccount(context.Background(), e.account0(t))

	if err != nil || !res.Full || res.Added != 1 || e.messages(t) != 3 || cursorOf(e.account0(t)) != "3" {
		t.Fatalf("res %+v, err %v, stored %d, cursor %q", res, err, e.messages(t), cursorOf(e.account0(t)))
	}
}

func TestAFailurePartWayLeavesTheCursorAndStoresNothing(t *testing.T) {
	boom := errors.New("provider hiccup")
	tests := []struct {
		name  string
		setup func(e *syncEnv)
	}{
		{"a message cannot be read", func(e *syncEnv) { e.fake.GetErrFor = map[string]error{"m3": boom} }},
		{"classification cannot be queued", func(e *syncEnv) { e.queue.err = boom }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSyncEnv(t)
			e.add(2, mail.Inbound)
			if _, err := e.syncer.SyncAccount(context.Background(), e.account); err != nil {
				t.Fatal(err)
			}
			before := cursorOf(e.account0(t))
			e.add(2, mail.Inbound)
			tt.setup(e)

			_, err := e.syncer.SyncAccount(context.Background(), e.account0(t))

			if !errors.Is(err, boom) || cursorOf(e.account0(t)) != before || e.messages(t) != 2 {
				t.Fatalf("err %v, cursor %q (was %q), stored %d; want nothing changed", err, cursorOf(e.account0(t)), before, e.messages(t))
			}
			e.fake.GetErrFor, e.queue.err = nil, nil
			res, retryErr := e.syncer.SyncAccount(context.Background(), e.account0(t))
			if retryErr != nil || res.Added != 2 || e.messages(t) != 4 {
				t.Fatalf("retry: %+v, %v, stored %d; want the same two messages read again", res, retryErr, e.messages(t))
			}
		})
	}
}

func TestAMessageDeletedBeforeItIsReadIsSkipped(t *testing.T) {
	e := newSyncEnv(t)
	e.add(2, mail.Inbound)
	e.fake.GetErrFor = map[string]error{"m1": mail.ErrNotFound}

	res, err := e.syncer.SyncAccount(context.Background(), e.account)

	if err != nil || res.Added != 1 || e.messages(t) != 1 {
		t.Fatalf("res %+v, err %v", res, err)
	}
}

func TestOnlyInboundMailIsQueuedForClassification(t *testing.T) {
	e := newSyncEnv(t)
	e.add(1, mail.Inbound)
	e.add(2, mail.Outbound)

	res, err := e.syncer.SyncAccount(context.Background(), e.account)

	if err != nil || res.Added != 3 || len(e.queue.ids) != 1 {
		t.Fatalf("res %+v, err %v, queued %d", res, err, len(e.queue.ids))
	}
	var direction string
	if err := e.pool.QueryRow(context.Background(), `SELECT direction FROM messages WHERE id = $1`, e.queue.ids[0]).Scan(&direction); err != nil || direction != "inbound" {
		t.Fatalf("queued a %q message, err %v", direction, err)
	}
}

func TestAFullSyncIsCappedAndFollowsPages(t *testing.T) {
	e := newSyncEnv(t)
	e.add(250, mail.Inbound)

	res, err := e.syncer.SyncAccount(context.Background(), e.account)

	if err != nil || res.Added != 250 {
		t.Fatalf("res %+v, err %v; want all 250 across three pages", res, err)
	}
	if n, _ := strconv.Atoi(cursorOf(e.account0(t))); n != 250 {
		t.Fatalf("cursor %q", cursorOf(e.account0(t)))
	}
}

func TestSyncAllSyncsEveryActiveAccountAndKeepsGoingAfterAFailure(t *testing.T) {
	e := newSyncEnv(t)
	e.add(1, mail.Inbound)
	other, _ := newAccountsOn(t, e.pool)
	second, err := other.Connect(context.Background(), uuid.New(), "gmail", "second@example.com", "1//second")
	if err != nil {
		t.Fatal(err)
	}
	disabled, _ := other.Connect(context.Background(), uuid.New(), "gmail", "off@example.com", "1//off")
	if _, err := e.pool.Exec(context.Background(), `UPDATE accounts SET status = 'disabled' WHERE id = $1`, disabled.ID); err != nil {
		t.Fatal(err)
	}
	// The first account's token is wrong for the fake's owner, so make its
	// provider fail while the second succeeds.
	failing := &perAccountFactory{fail: map[string]error{refresh: errors.New("down")}, ok: e.fake}
	syncer := app.NewSyncer(e.pool, other, failing, e.queue, slog.New(slog.DiscardHandler), nil)

	err = syncer.SyncAll(context.Background())

	if err == nil {
		t.Fatal("want the failure reported")
	}
	if acc, _ := store.New(e.pool).GetAccount(context.Background(), second.OwnerID, second.ID); cursorOf(acc) == "" {
		t.Fatal("the healthy account was not synced after the other one failed")
	}
	if acc, _ := store.New(e.pool).GetAccount(context.Background(), disabled.OwnerID, disabled.ID); cursorOf(acc) != "" {
		t.Fatal("a disabled account was synced")
	}
}

// perAccountFactory fails providers opened with certain tokens.
type perAccountFactory struct {
	fail map[string]error
	ok   mail.Factory
}

func (f *perAccountFactory) Provider(token string) mail.Provider {
	if err := f.fail[token]; err != nil {
		return &mailtest.Fake{Err: err}
	}
	return f.ok.Provider(token)
}
