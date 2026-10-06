package app_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/pkg/hanko"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
)

// scriptedMailer answers each Send with the next error of its script; a script
// that has run out answers nil.
type scriptedMailer struct {
	mu     sync.Mutex
	script []error
	calls  []app.SendRequest
}

func (m *scriptedMailer) Send(_ context.Context, req app.SendRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if len(m.script) == 0 {
		return nil
	}
	err := m.script[0]
	m.script = m.script[1:]
	return err
}

func newMailApproval(t *testing.T, script ...error) (*approval, *scriptedMailer) {
	t.Helper()
	a := newApproval(t)
	mailer := &scriptedMailer{script: script}
	_, priv, _ := a.pubPriv()
	approver, err := app.NewApprover(a.env.pool, priv, keyID, func() time.Time { return a.env.now },
		app.WithMailer(mailer, discardLog()), app.WithRetryBackoff(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	a.approver = approver
	return a, mailer
}

func (a *approval) approveEmail(t *testing.T) (db.Draft, error) {
	t.Helper()
	d, subject, body := a.pendingDraft(t, "email", strp("jobs@lumen.example"))
	_, err := a.approver.Approve(a.ctx(), app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: hanko.BodyDigest(subject, body)})
	return d, err
}

func (a *approval) stateOf(t *testing.T, d db.Draft) string {
	t.Helper()
	got, err := store.New(a.env.pool).GetDraft(context.Background(), a.env.owner, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got.State
}

func TestApprovingAnEmailDraftHandsItsExactTextToTheMailer(t *testing.T) {
	a, mailer := newMailApproval(t)

	d, err := a.approveEmail(t)

	if err != nil || len(mailer.calls) != 1 {
		t.Fatalf("err %v, calls %d", err, len(mailer.calls))
	}
	req := mailer.calls[0]
	if req.Token == "" || req.DraftID != d.ID || req.Version != 1 || len(req.To) != 1 || req.To[0] != "jobs@lumen.example" ||
		req.Subject != "Hello Lumen" || req.Body != "Dear Lumen team,\nI would like to help." {
		t.Fatalf("mailer got %+v", req)
	}
	if got := a.stateOf(t, d); got != "approved" {
		t.Fatalf("state %q: only tsubame's draft.sent may mark it sent", got)
	}
}

func TestTheMailerIsToldWhichJobOrContactTheDraftIsAbout(t *testing.T) {
	a, mailer := newMailApproval(t)
	target := uuid.New()
	d, subject, body := a.pendingDraft(t, "email", strp("jobs@lumen.example"))
	if _, err := a.env.pool.Exec(context.Background(), `UPDATE drafts SET target_type = 'contact', target_id = $2 WHERE id = $1`, d.ID, target); err != nil {
		t.Fatal(err)
	}

	_, err := a.approver.Approve(a.ctx(), app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: hanko.BodyDigest(subject, body)})

	if req := mailer.calls[0]; err != nil || req.ContactID != target.String() || req.JobID != "" {
		t.Fatalf("err %v, request %+v", err, req)
	}
}

func TestACopyOnlyDraftIsNeverHandedToTheMailer(t *testing.T) {
	a, mailer := newMailApproval(t)
	d, subject, body := a.pendingDraft(t, "linkedin", nil)

	res, err := a.approver.Approve(a.ctx(), app.ApproveInput{DraftID: d.ID.String(), Version: 1, BodySHA256: hanko.BodyDigest(subject, body)})

	if err != nil || !res.CopyReady || len(mailer.calls) != 0 {
		t.Fatalf("err %v, copy ready %v, calls %d", err, res.CopyReady, len(mailer.calls))
	}
}

func TestADefiniteRefusalPutsTheDraftBackAndKeepsTheApprovalRecord(t *testing.T) {
	a, mailer := newMailApproval(t, app.ErrMailRefused)

	d, err := a.approveEmail(t)

	if !errors.Is(err, app.ErrSendRefused) || len(mailer.calls) != 1 {
		t.Fatalf("err %v, calls %d; a refusal is final and must not be retried", err, len(mailer.calls))
	}
	if got := a.stateOf(t, d); got != "pending" {
		t.Fatalf("state %q, want pending so the owner can approve again", got)
	}
	if n := a.env.count(t, `SELECT count(*) FROM approvals`); n != 1 {
		t.Fatalf("got %d approvals; the record of what was approved must stay", n)
	}
}

func TestAProviderFailureLeavesTheDraftForTheEventToPutBack(t *testing.T) {
	a, _ := newMailApproval(t, app.ErrMailFailed)

	d, err := a.approveEmail(t)

	if !errors.Is(err, app.ErrSendFailed) {
		t.Fatalf("got %v", err)
	}
	if got := a.stateOf(t, d); got != "approved" {
		t.Fatalf("state %q: only draft.send_failed may put it back", got)
	}
}

func TestATokenAlreadyUsedMeansTheSendIsUnderWay(t *testing.T) {
	a, _ := newMailApproval(t, app.ErrMailInFlight)

	d, err := a.approveEmail(t)

	if err != nil || a.stateOf(t, d) != "approved" {
		t.Fatalf("err %v, state %q", err, a.stateOf(t, d))
	}
}

func TestAnUnreachableMailServiceIsRetriedWithTheSameToken(t *testing.T) {
	a, mailer := newMailApproval(t, app.ErrMailUnreachable, app.ErrMailUnreachable)

	d, err := a.approveEmail(t)

	if err != nil || len(mailer.calls) != 3 {
		t.Fatalf("err %v, calls %d; want success on the third try", err, len(mailer.calls))
	}
	if mailer.calls[0].Token != mailer.calls[1].Token || mailer.calls[1].Token != mailer.calls[2].Token {
		t.Fatal("a retry must use the same token, so a send that got through is refused as spent")
	}
	if a.stateOf(t, d) != "approved" {
		t.Fatalf("state %q", a.stateOf(t, d))
	}
}

func TestAMailServiceThatStaysUnreachableEndsWithTheDraftBackInTheQueue(t *testing.T) {
	a, mailer := newMailApproval(t, app.ErrMailUnreachable, app.ErrMailUnreachable, app.ErrMailUnreachable)

	d, err := a.approveEmail(t)

	if !errors.Is(err, app.ErrSendUnavailable) || len(mailer.calls) != 3 {
		t.Fatalf("err %v, calls %d", err, len(mailer.calls))
	}
	if got := a.stateOf(t, d); got != "pending" {
		t.Fatalf("state %q, want pending", got)
	}
}

func TestAnUnrecognisedMailerErrorLeavesTheDraftApprovedBecauseItMayHaveGoneOut(t *testing.T) {
	a, mailer := newMailApproval(t, errors.New("deadline exceeded"))

	d, err := a.approveEmail(t)

	if !errors.Is(err, app.ErrSendUnknown) || len(mailer.calls) != 1 {
		t.Fatalf("err %v, calls %d; an unknown outcome must not be retried", err, len(mailer.calls))
	}
	if got := a.stateOf(t, d); got != "approved" {
		t.Fatalf("state %q: not knowing is not a reason to say it was not sent", got)
	}
}

func TestRecordSentAndFailedAreIdempotentAndIgnoreStaleReports(t *testing.T) {
	a := newApproval(t)
	svc := app.NewService(a.env.pool, nil, func() time.Time { return a.env.now })
	ctx := context.Background()
	owner := a.env.owner
	run := func(fn func(tx pgx.Tx) error) {
		t.Helper()
		tx, err := a.env.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	d, _, _ := a.pendingDraft(t, "email", strp("jobs@lumen.example"))
	if _, err := a.env.pool.Exec(ctx, `UPDATE drafts SET state = 'approved' WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}

	run(func(tx pgx.Tx) error { return svc.RecordSendFailed(ctx, tx, owner, d.ID, 9) }) // stale version
	staleState := a.stateOf(t, d)
	run(func(tx pgx.Tx) error { return svc.RecordSent(ctx, tx, owner, d.ID, 1) })
	run(func(tx pgx.Tx) error { return svc.RecordSent(ctx, tx, owner, d.ID, 1) }) // again
	run(func(tx pgx.Tx) error { return svc.RecordSendFailed(ctx, tx, owner, d.ID, 1) })
	run(func(tx pgx.Tx) error { return svc.RecordSent(ctx, tx, owner, uuid.New(), 1) }) // a draft that is gone

	if staleState != "approved" || a.stateOf(t, d) != "sent" {
		t.Fatalf("stale report left %q, final state %q; want approved then sent, and a failure after sent must not undo it", staleState, a.stateOf(t, d))
	}
}
