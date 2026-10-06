package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
)

const (
	// sendAttempts is how many times a send that could not reach tsubame is
	// tried. Retrying with the same token is safe: a token works once, so a
	// retry that reaches tsubame after an earlier one did is refused as spent.
	sendAttempts = 3
	// sendBackoff is the wait before the first retry; it doubles.
	sendBackoff = 200 * time.Millisecond
)

// How tsubame answered a send. Mailer implementations wrap one of these.
var (
	// ErrMailRefused means tsubame refused the send and sent nothing, for
	// example because no mail account is connected.
	ErrMailRefused = errors.New("app: mail send refused")
	// ErrMailFailed means tsubame tried and the provider did not send. It has
	// announced that with draft.send_failed.
	ErrMailFailed = errors.New("app: mail provider did not send")
	// ErrMailInFlight means the token was already used: an earlier attempt
	// reached tsubame and its outcome will arrive as an event.
	ErrMailInFlight = errors.New("app: mail send already under way")
	// ErrMailUnreachable means tsubame could not be reached.
	ErrMailUnreachable = errors.New("app: mail service unreachable")
)

// What Approve returns when the mail does not go out. In every case the
// approval itself stands; these say what happened to the send.
var (
	// ErrSendRefused: nothing was sent, and the draft is back in the queue.
	ErrSendRefused = errors.New("app: the mail could not be sent")
	// ErrSendFailed: the provider did not send. draft.send_failed puts the draft
	// back in the queue.
	ErrSendFailed = errors.New("app: the mail provider did not send the mail")
	// ErrSendUnavailable: tsubame could not be reached after retries, so nothing
	// was sent, and the draft is back in the queue.
	ErrSendUnavailable = errors.New("app: the mail service is unavailable")
	// ErrSendUnknown: it is not known whether the mail went out. The draft stays
	// approved until tsubame reports the outcome.
	ErrSendUnknown = errors.New("app: could not confirm whether the mail was sent")
)

// SendRequest is one approved draft for tsubame to send.
type SendRequest struct {
	Token     string
	DraftID   uuid.UUID
	Version   int32
	To        []string
	Subject   string
	Body      string
	ContactID string
	JobID     string
}

// Mailer hands an approved draft to tsubame. Its errors wrap the ErrMail*
// values.
type Mailer interface {
	Send(ctx context.Context, req SendRequest) error
}

// ApproverOption configures an Approver.
type ApproverOption func(*Approver)

// WithMailer makes Approve send approved email drafts. Without one, nothing is
// sent.
func WithMailer(m Mailer, log *slog.Logger) ApproverOption {
	return func(a *Approver) { a.mailer, a.log = m, log }
}

// WithRetryBackoff sets the wait before the first retry of a send that could not
// reach tsubame; it doubles each time.
func WithRetryBackoff(d time.Duration) ApproverOption {
	return func(a *Approver) { a.backoff = d }
}

// dispatch sends an approved email draft and, when tsubame definitely sent
// nothing, puts the draft back in the queue. It never decides that a mail was
// sent: that is draft.sent, which tsubame emits.
func (a *Approver) dispatch(ctx context.Context, owner uuid.UUID, res ApproveResult) error {
	if a.mailer == nil || res.CopyReady || res.Draft.Recipient == nil {
		return nil
	}
	req := SendRequest{
		Token: res.Token, DraftID: res.Draft.ID, Version: res.Version.Version, To: []string{*res.Draft.Recipient},
		Subject: deref(res.Version.Subject), Body: res.Version.Body, ContactID: targetOf(res.Draft, domain.TargetContact),
		JobID: targetOf(res.Draft, domain.TargetJob),
	}
	err := a.sendWithRetry(ctx, req)
	switch {
	case err == nil, errors.Is(err, ErrMailInFlight):
		return nil
	case errors.Is(err, ErrMailFailed):
		return fmt.Errorf("%w", ErrSendFailed) // draft.send_failed brings the draft back
	case errors.Is(err, ErrMailRefused):
		a.revert(ctx, owner, res)
		return fmt.Errorf("%w", ErrSendRefused)
	case errors.Is(err, ErrMailUnreachable):
		a.revert(ctx, owner, res)
		return fmt.Errorf("%w", ErrSendUnavailable)
	default:
		a.log.Error("send outcome unknown", slog.String("draft_id", res.Draft.ID.String()), slog.Any("error", err))
		return fmt.Errorf("%w", ErrSendUnknown)
	}
}

// sendWithRetry retries only when tsubame could not be reached.
func (a *Approver) sendWithRetry(ctx context.Context, req SendRequest) error {
	var err error
	wait := a.backoff
	for attempt := 1; attempt <= sendAttempts; attempt++ {
		if err = a.mailer.Send(ctx, req); err == nil || !errors.Is(err, ErrMailUnreachable) {
			return err
		}
		if attempt == sendAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
	return err
}

// revert puts an approved draft back to pending after a send that did not go
// out, so the owner can approve it again. It works on a context that outlives
// the caller's, and logs rather than returns its own failure: the send already
// failed, and a draft left approved is brought back by a later event or edit.
func (a *Approver) revert(ctx context.Context, owner uuid.UUID, res ApproveResult) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revertTimeout)
	defer cancel()
	err := postgres.InTx(ctx, a.pool, func(tx pgx.Tx) error {
		_, err := recordSendFailed(ctx, store.New(tx), a.now(), owner, res.Draft.ID, res.Version.Version)
		return err
	})
	if err != nil {
		a.log.Error("put draft back to pending", slog.String("draft_id", res.Draft.ID.String()), slog.Any("error", err))
	}
}

// revertTimeout bounds the write that puts a draft back.
const revertTimeout = 15 * time.Second

// targetOf returns the draft's target id when it is of the given type.
func targetOf(d db.Draft, t domain.TargetType) string {
	if d.TargetID == nil || domain.TargetType(d.TargetType) != t {
		return ""
	}
	return d.TargetID.String()
}
