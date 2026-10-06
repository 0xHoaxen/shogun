package app_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail/mailtest"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

type recEnv struct {
	pool    *pgxpool.Pool
	sender  *app.Sender
	fake    *mailtest.Fake
	account db.Account
	now     time.Time
}

func newRecEnv(t *testing.T) *recEnv {
	t.Helper()
	accounts, pool := newAccounts(t)
	owner := uuid.New()
	acc, err := accounts.Connect(context.Background(), owner, "gmail", "me@example.com", refresh)
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _ := ed25519.GenerateKey(nil)
	fake := &mailtest.Fake{Address: "me@example.com"}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	sender, err := app.NewSender(pool, accounts, fake, map[string]ed25519.PublicKey{"k": pub}, slog.New(slog.DiscardHandler), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return &recEnv{pool: pool, sender: sender, fake: fake, account: acc, now: now}
}

// openSend stores a send left in "sending", started age ago.
func (e *recEnv) openSend(t *testing.T, age time.Duration) db.Send {
	t.Helper()
	contact, job := uuid.New(), uuid.New()
	row, err := store.New(e.pool).InsertSend(context.Background(), db.InsertSendParams{
		ID: store.NewID(), OwnerID: e.account.OwnerID, AccountID: e.account.ID, DraftID: uuid.New(), DraftVersion: 2,
		ContactID: &contact, JobID: &job, TokenJti: uuid.New(), ToAddrs: []string{"jobs@lumen.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE sends SET created_at = $2 WHERE id = $1`, row.ID, e.now.Add(-age)); err != nil {
		t.Fatal(err)
	}
	row.CreatedAt = e.now.Add(-age)
	return row
}

// inSent puts a message with the draft header into the provider's Sent mail.
func (e *recEnv) inSent(row db.Send, sentAt time.Time) {
	e.fake.Messages = append(e.fake.Messages, mail.Message{
		ID: "sent-" + row.ID.String()[:8], Direction: mail.Outbound,
		DraftID: row.DraftID.String() + ":2", ReceivedAt: sentAt,
	})
}

func (e *recEnv) state(t *testing.T, row db.Send) (status string, reason, providerID *string) {
	t.Helper()
	if err := e.pool.QueryRow(context.Background(), `SELECT status, error, provider_message_id FROM sends WHERE id = $1`, row.ID).Scan(&status, &reason, &providerID); err != nil {
		t.Fatal(err)
	}
	return status, reason, providerID
}

func (e *recEnv) events(t *testing.T, typ string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE type = $1`, typ).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReconcileRecordsAMailFoundInSentAsSent(t *testing.T) {
	e := newRecEnv(t)
	row := e.openSend(t, 3*time.Minute)
	sentAt := e.now.Add(-150 * time.Second)
	e.inSent(row, sentAt)

	res, err := e.sender.Reconcile(context.Background())

	status, _, providerID := e.state(t, row)
	if err != nil || res.Recovered != 1 || res.Failed != 0 || status != "sent" || providerID == nil {
		t.Fatalf("res %+v, err %v, status %q", res, err, status)
	}
	if e.events(t, "draft.sent") != 1 || e.events(t, "draft.send_failed") != 0 {
		t.Fatalf("sent %d, failed %d events", e.events(t, "draft.sent"), e.events(t, "draft.send_failed"))
	}
	var contact, job *uuid.UUID
	var at *time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT contact_id, job_id, sent_at FROM sends WHERE id = $1`, row.ID).Scan(&contact, &job, &at); err != nil ||
		contact == nil || job == nil || at == nil || !at.Equal(sentAt) {
		t.Fatalf("contact %v, job %v, sent_at %v, err %v; want the ids kept and the time of the mail", contact, job, at, err)
	}
}

func TestReconcileGivesUpOnAMailThatIsNotThereOnceItIsOldEnough(t *testing.T) {
	e := newRecEnv(t)
	young := e.openSend(t, 5*time.Minute)
	old := e.openSend(t, 11*time.Minute)

	res, err := e.sender.Reconcile(context.Background())

	youngStatus, _, _ := e.state(t, young)
	oldStatus, oldReason, _ := e.state(t, old)
	if err != nil || res.Failed != 1 || res.Recovered != 0 {
		t.Fatalf("res %+v, err %v", res, err)
	}
	if youngStatus != "sending" {
		t.Fatalf("a five minute old send was decided: %q", youngStatus)
	}
	if oldStatus != "failed" || oldReason == nil || *oldReason != "not_found_in_sent" {
		t.Fatalf("old send: %q %v", oldStatus, oldReason)
	}
	if e.events(t, "draft.send_failed") != 1 || e.events(t, "draft.sent") != 0 {
		t.Fatalf("sent %d, failed %d events", e.events(t, "draft.sent"), e.events(t, "draft.send_failed"))
	}
}

func TestReconcileLeavesASendWhoseCallMayStillBeFinishing(t *testing.T) {
	e := newRecEnv(t)
	row := e.openSend(t, 30*time.Second)
	e.inSent(row, e.now)

	res, err := e.sender.Reconcile(context.Background())

	if status, _, _ := e.state(t, row); err != nil || res.Recovered != 0 || status != "sending" {
		t.Fatalf("res %+v, err %v, status %q", res, err, status)
	}
}

func TestReconcileLeavesEverythingAloneWhenTheProviderCannotBeAsked(t *testing.T) {
	boom := errors.New("provider down")
	tests := []struct {
		name  string
		setup func(e *recEnv)
	}{
		{"the provider fails", func(e *recEnv) { e.fake.Err = boom }},
		{"the account needs reauth", func(e *recEnv) {
			if _, err := e.pool.Exec(context.Background(), `UPDATE accounts SET status = 'reauth_required'`); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			row := e.openSend(t, 30*time.Minute) // old enough to give up, but it cannot be checked
			tt.setup(e)

			res, err := e.sender.Reconcile(context.Background())

			if status, _, _ := e.state(t, row); err != nil || res.Failed != 0 || res.Recovered != 0 || status != "sending" {
				t.Fatalf("res %+v, err %v, status %q; an unanswerable provider must not decide anything", res, err, status)
			}
			if e.events(t, "draft.send_failed") != 0 {
				t.Fatal("a failure was announced without evidence")
			}
		})
	}
}

func TestReconcileIsRepeatableAndNeverSends(t *testing.T) {
	e := newRecEnv(t)
	found := e.openSend(t, 4*time.Minute)
	e.inSent(found, e.now.Add(-time.Minute))
	e.openSend(t, 12*time.Minute)

	first, err := e.sender.Reconcile(context.Background())
	second, secondErr := e.sender.Reconcile(context.Background())

	if err != nil || secondErr != nil || first.Recovered != 1 || first.Failed != 1 || second.Recovered != 0 || second.Failed != 0 {
		t.Fatalf("first %+v %v, second %+v %v", first, err, second, secondErr)
	}
	if e.events(t, "draft.sent") != 1 || e.events(t, "draft.send_failed") != 1 {
		t.Fatalf("sent %d, failed %d events; want one each", e.events(t, "draft.sent"), e.events(t, "draft.send_failed"))
	}
	if e.fake.SentCount() != 0 {
		t.Fatalf("the reconciler sent %d mails", e.fake.SentCount())
	}
}

func TestReconcileLooksAtEachAccountsSentMailOncePerRunAndOnlyMatchesTheDraftAndVersion(t *testing.T) {
	e := newRecEnv(t)
	row := e.openSend(t, 3*time.Minute)
	e.fake.Messages = append(e.fake.Messages,
		mail.Message{ID: "other-draft", Direction: mail.Outbound, DraftID: uuid.NewString() + ":2", ReceivedAt: e.now},
		mail.Message{ID: "other-version", Direction: mail.Outbound, DraftID: row.DraftID.String() + ":1", ReceivedAt: e.now},
		mail.Message{ID: "no-header", Direction: mail.Outbound, ReceivedAt: e.now},
	)

	res, err := e.sender.Reconcile(context.Background())

	if status, _, _ := e.state(t, row); err != nil || res.Recovered != 0 || status != "sending" {
		t.Fatalf("res %+v, err %v, status %q; mail of another draft or version must not count", res, err, status)
	}
}
