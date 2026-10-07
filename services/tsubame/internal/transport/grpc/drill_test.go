package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
)

// failRecordingSentMail makes every update that marks a send "sent" fail, as a
// crash or a lost database connection would right after Gmail accepted the mail.
func (h *harness) failRecordingSentMail(t *testing.T) {
	t.Helper()
	_, err := h.pool.Exec(context.Background(), `
		CREATE FUNCTION drill_refuse_sent() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'drill: cannot record the sent mail'; END $$;
		CREATE TRIGGER drill_refuse_sent BEFORE UPDATE ON sends
			FOR EACH ROW WHEN (NEW.status = 'sent') EXECUTE FUNCTION drill_refuse_sent();`)
	if err != nil {
		t.Fatalf("install fault: %v", err)
	}
}

func (h *harness) recordSentMailAgain(t *testing.T) {
	t.Helper()
	if _, err := h.pool.Exec(context.Background(), `DROP TRIGGER drill_refuse_sent ON sends`); err != nil {
		t.Fatalf("remove fault: %v", err)
	}
}

func (h *harness) sendStatus(t *testing.T) string {
	t.Helper()
	var status string
	if err := h.pool.QueryRow(context.Background(), `SELECT status FROM sends`).Scan(&status); err != nil {
		t.Fatalf("send row: %v", err)
	}
	return status
}

func TestDrillGmailAcceptsTheMailButItCannotBeRecordedSendsOnceAndRecovers(t *testing.T) {
	// Arrange: an approved draft; the database refuses to record the send.
	h := newHarness(t)
	h.connectMail(t)
	draft := uuid.NewString()
	h.failRecordingSentMail(t)

	// Act: the send. Gmail takes the mail, then the record fails.
	_, err := h.send(t, h.token(t, draft, nil), draft)

	// Assert: the caller is told it failed, the mail is out once and the row is open.
	if err == nil {
		t.Fatal("want an error when the sent mail could not be recorded")
	}
	if h.mail.SentCount() != 1 || h.sendStatus(t) != "sending" {
		t.Fatalf("sent %d mails and the row is %q, want 1 and sending", h.mail.SentCount(), h.sendStatus(t))
	}

	// Retrying, even with a freshly minted approval, must not send a second mail.
	_, again := h.send(t, h.token(t, draft, nil), draft)
	if again == nil || h.mail.SentCount() != 1 {
		t.Fatalf("retry returned %v and %d mails are out, want a refusal and 1", again, h.mail.SentCount())
	}

	// The database comes back. The row is dated by the harness clock, which the
	// reconciler also reads; before the grace period it leaves the row alone.
	h.recordSentMailAgain(t)
	if _, err := h.pool.Exec(context.Background(), `UPDATE sends SET created_at = $1`, h.clock()); err != nil {
		t.Fatalf("date the send: %v", err)
	}
	if res, err := h.sender.Reconcile(context.Background()); err != nil || res.Recovered != 0 {
		t.Fatalf("early Reconcile = %+v, %v, want nothing recovered", res, err)
	}

	// After it, the mail is found in Sent and recorded once, without sending.
	h.advance(2 * time.Minute)
	for run, wantRecovered := range []int{1, 0} {
		res, err := h.sender.Reconcile(context.Background())
		if err != nil || res.Recovered != wantRecovered || res.Failed != 0 {
			t.Fatalf("Reconcile run %d = %+v, %v, want %d recovered and none failed", run+1, res, err, wantRecovered)
		}
	}
	if h.sendStatus(t) != "sent" {
		t.Errorf("row is %q after the reconciler, want sent", h.sendStatus(t))
	}
	if h.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.sent'`) != 1 || h.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.send_failed'`) != 0 {
		t.Error("want exactly one draft.sent and no draft.send_failed")
	}
	if h.mail.SentCount() != 1 {
		t.Errorf("%d mails went out in total, want 1", h.mail.SentCount())
	}
}

func TestDrillGmailErrorAfterTheTokenIsUsedAnnouncesOnceAndNeverReusesTheToken(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.connectMail(t)
	draft := uuid.NewString()
	tok := h.token(t, draft, nil)
	h.mail.SendErr = &mail.APIError{Status: 500, Message: "backend error"}

	// Act: the same token is presented three times.
	_, first := h.send(t, tok, draft)
	_, second := h.send(t, tok, draft)
	_, third := h.send(t, tok, draft)

	// Assert: one failure announced, the token is spent, nothing went out.
	requireStatus(t, first, codes.FailedPrecondition, "SEND_FAILED")
	requireStatus(t, second, codes.PermissionDenied, "APPROVAL_ALREADY_USED")
	requireStatus(t, third, codes.PermissionDenied, "APPROVAL_ALREADY_USED")
	if h.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.send_failed'`) != 1 {
		t.Error("want exactly one draft.send_failed")
	}
	if h.mail.SentCount() != 0 {
		t.Errorf("%d mails went out, want none", h.mail.SentCount())
	}
}
