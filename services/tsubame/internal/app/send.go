package app

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/hanko"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

const (
	eventDraftSent       = "draft.sent"
	eventDraftSendFailed = "draft.send_failed"

	hankoAudience = "tsubame"
	hankoIssuer   = "fude"

	maxRecipients = 20
	maxSubject    = 300
	maxBody       = 20000
	// settleTimeout bounds the write that records an outcome, which runs even
	// when the caller has gone away.
	settleTimeout = 15 * time.Second
)

// Reasons recorded when a send does not go out. They are codes, never text
// from the mail.
const (
	ReasonProviderError   = "provider_error"
	ReasonAuthRevoked     = "auth_revoked"
	ReasonInvalidMessage  = "invalid_message"
	ReasonAccountUnusable = "account_unusable"
	ReasonNotInSent       = "not_found_in_sent"
)

// Errors Send returns.
var (
	// ErrNotApproved means the Hanko is missing, invalid, expired, for another
	// owner, or does not cover exactly this draft version, text and recipients.
	ErrNotApproved = errors.New("app: send is not approved")
	// ErrTokenSpent means the Hanko was already used.
	ErrTokenSpent = errors.New("app: approval already used")
	// ErrAlreadySent means this draft version is already sent or being sent.
	ErrAlreadySent = errors.New("app: draft version already sent")
	// ErrNoAccount means the owner has no mail account that can send.
	ErrNoAccount = errors.New("app: no connected mail account can send")
	// ErrSendFailed means the provider did not send the mail. The draft needs a
	// new approval; draft.send_failed says so.
	ErrSendFailed = errors.New("app: provider did not send the mail")
)

// SendInput is what fude asks tsubame to send.
type SendInput struct {
	Hanko     string
	DraftID   string
	Version   int32
	To        []string
	Subject   string
	Body      string
	ContactID string
	JobID     string
}

// Sender sends approved drafts. It is the only code that sends mail, and it
// sends nothing without a Hanko that covers exactly what it is asked to send.
type Sender struct {
	pool     *pgxpool.Pool
	accounts *Accounts
	factory  mail.Factory
	keys     map[string]ed25519.PublicKey
	log      *slog.Logger
	now      func() time.Time
}

// NewSender returns a Sender that trusts tokens signed by keys (by key id). A
// nil now means time.Now.
func NewSender(pool *pgxpool.Pool, accounts *Accounts, factory mail.Factory, keys map[string]ed25519.PublicKey, log *slog.Logger, now func() time.Time) (*Sender, error) {
	if len(keys) == 0 {
		return nil, errors.New("sender: at least one Hanko verification key is required")
	}
	if now == nil {
		now = time.Now
	}
	return &Sender{pool: pool, accounts: accounts, factory: factory, keys: keys, log: log, now: now}, nil
}

// Send verifies the Hanko, records the send, hands the mail to the provider
// once, and records the outcome. The record is committed before the provider is
// called, so a crash in between can never lead to a second send: the leftover
// row is resolved by the reconciler. The outcome is also written as an event
// (draft.sent or draft.send_failed) that fude acts on.
func (s *Sender) Send(ctx context.Context, in SendInput) (string, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return "", err
	}
	draftID, err := validateSend(in)
	if err != nil {
		return "", err
	}
	claims, err := s.verify(in, owner)
	if err != nil {
		return "", err
	}
	jti, err := uuid.Parse(claims.ID)
	if err != nil {
		return "", ErrNotApproved
	}
	acc, err := s.sendingAccount(ctx, owner)
	if err != nil {
		return "", err
	}
	row, err := s.reserve(ctx, owner, acc, draftID, jti, in)
	if err != nil {
		return "", err
	}

	provider, err := s.accounts.Provider(ctx, owner, acc.ID, s.factory, s.log)
	if err != nil {
		s.fail(ctx, row, ReasonAccountUnusable)
		return "", fmt.Errorf("%w: %s", ErrSendFailed, ReasonAccountUnusable)
	}
	sent, err := provider.Send(ctx, mail.Outgoing{
		From: acc.Address, To: in.To, Subject: in.Subject, Body: in.Body, DraftID: draftHeader(in.DraftID, in.Version),
	})
	if err != nil {
		reason := sendFailureReason(err)
		s.fail(ctx, row, reason)
		return "", fmt.Errorf("%w: %s", ErrSendFailed, reason)
	}
	if err := s.succeed(ctx, row, sent.ID); err != nil {
		// The mail is out but the record is not. The row stays "sending" and the
		// reconciler finds the mail in Sent and records it.
		return sent.ID, fmt.Errorf("record sent mail: %w", err)
	}
	return sent.ID, nil
}

func validateSend(in SendInput) (uuid.UUID, error) {
	id, err := parseUUID(in.DraftID)
	switch {
	case err != nil:
		return uuid.Nil, invalidArg("draft_id is not a valid id")
	case in.Hanko == "":
		return uuid.Nil, ErrNotApproved
	case in.Version < 1:
		return uuid.Nil, invalidArg("version is required")
	case len(in.To) == 0 || len(in.To) > maxRecipients:
		return uuid.Nil, invalidArg("a send needs between 1 and %d recipients", maxRecipients)
	case in.Body == "" || len(in.Body) > maxBody:
		return uuid.Nil, invalidArg("body is required and at most %d bytes", maxBody)
	case len(in.Subject) > maxSubject:
		return uuid.Nil, invalidArg("subject is at most %d bytes", maxSubject)
	}
	return id, nil
}

// verify checks the Hanko covers exactly this draft, version, text and
// recipients, was signed by fude for tsubame, and was issued for this owner.
// Every failure is the same error, so nothing is learned about why.
func (s *Sender) verify(in SendInput, owner uuid.UUID) (hanko.Claims, error) {
	claims, err := hanko.Verify(in.Hanko, s.keys, hanko.Expected{
		Audience: hankoAudience, Issuer: hankoIssuer, DraftID: in.DraftID, Version: int64(in.Version),
		BodySHA256: hex.EncodeToString(hanko.BodyDigest(in.Subject, in.Body)),
		RcptSHA256: hex.EncodeToString(hanko.RecipientDigest(in.To)),
	}, hanko.WithClock(s.now))
	if err != nil || claims.Subject != owner.String() {
		s.log.Warn("send refused: hanko not valid", slog.String("draft_id", in.DraftID), slog.Bool("signature_ok", err == nil))
		return hanko.Claims{}, ErrNotApproved
	}
	return claims, nil
}

// sendingAccount picks the owner's first active Gmail account.
func (s *Sender) sendingAccount(ctx context.Context, owner uuid.UUID) (db.Account, error) {
	accounts, err := s.accounts.List(ctx, owner)
	if err != nil {
		return db.Account{}, err
	}
	for _, a := range accounts {
		if a.Provider == ProviderGmail && a.Status == "active" {
			return a, nil
		}
	}
	return db.Account{}, ErrNoAccount
}

// reserve commits the send record, which spends the token.
func (s *Sender) reserve(ctx context.Context, owner uuid.UUID, acc db.Account, draftID, jti uuid.UUID, in SendInput) (db.Send, error) {
	row, err := store.New(s.pool).InsertSend(ctx, db.InsertSendParams{
		ID: store.NewID(), OwnerID: owner, AccountID: acc.ID, DraftID: draftID, DraftVersion: in.Version,
		ContactID: optionalUUID(in.ContactID), JobID: optionalUUID(in.JobID), TokenJti: jti, ToAddrs: in.To,
	})
	if errors.Is(err, store.ErrDuplicate) {
		if isTokenConflict(err) {
			return db.Send{}, ErrTokenSpent
		}
		return db.Send{}, ErrAlreadySent
	}
	return row, err
}

// succeed records the sent mail and draft.sent together.
func (s *Sender) succeed(ctx context.Context, row db.Send, providerID string) error {
	ctx, cancel := settleContext(ctx)
	defer cancel()
	sentAt := s.now()
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		changed, err := store.New(tx).MarkSendSent(ctx, row.ID, providerID, sentAt)
		if err != nil || !changed {
			return err
		}
		return writeSent(ctx, tx, row, sentAt)
	})
}

// fail records the failed send and draft.send_failed together. It logs rather
// than returns its own failure: the send already failed, and the reconciler
// closes a row that stays open.
func (s *Sender) fail(ctx context.Context, row db.Send, reason string) {
	ctx, cancel := settleContext(ctx)
	defer cancel()
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		changed, err := store.New(tx).MarkSendFailed(ctx, row.ID, reason)
		if err != nil || !changed {
			return err
		}
		return writeSendFailed(ctx, tx, row, reason)
	})
	if err != nil {
		s.log.Error("record failed send", slog.String("send_id", row.ID.String()), slog.Any("error", err))
	}
}

func writeSent(ctx context.Context, tx pgx.Tx, row db.Send, sentAt time.Time) error {
	_, err := outbox.Write(ctx, tx, eventSource, eventDraftSent, row.DraftID.String(), &tsubamev1.DraftSent{
		OwnerId: row.OwnerID.String(), DraftId: row.DraftID.String(), Version: row.DraftVersion,
		ContactId: idString(row.ContactID), JobId: idString(row.JobID), SentAt: timestamppb.New(sentAt),
	})
	if err != nil {
		return fmt.Errorf("write %s event: %w", eventDraftSent, err)
	}
	return nil
}

func writeSendFailed(ctx context.Context, tx pgx.Tx, row db.Send, reason string) error {
	_, err := outbox.Write(ctx, tx, eventSource, eventDraftSendFailed, row.DraftID.String(), &tsubamev1.DraftSendFailed{
		OwnerId: row.OwnerID.String(), DraftId: row.DraftID.String(), Version: row.DraftVersion, Reason: reason,
	})
	if err != nil {
		return fmt.Errorf("write %s event: %w", eventDraftSendFailed, err)
	}
	return nil
}

func sendFailureReason(err error) string {
	switch {
	case errors.Is(err, mail.ErrAuthRevoked):
		return ReasonAuthRevoked
	case errors.Is(err, mail.ErrInvalidMessage):
		return ReasonInvalidMessage
	default:
		return ReasonProviderError
	}
}

// draftHeader is the value of the X-Shogun-Draft header, which the reconciler
// looks for in Sent mail.
func draftHeader(draftID string, version int32) string { return fmt.Sprintf("%s:%d", draftID, version) }

func settleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
}

func parseUUID(s string) (uuid.UUID, error) { return uuid.Parse(s) }

func optionalUUID(s string) *uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}

// isTokenConflict reports whether a duplicate send was caused by the token id
// rather than by the draft version already having a live send.
func isTokenConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "token_jti")
}

// InvalidArgumentError reports input the use case refuses.
type InvalidArgumentError struct{ Msg string }

func (e *InvalidArgumentError) Error() string { return e.Msg }

func invalidArg(format string, args ...any) error {
	return &InvalidArgumentError{Msg: fmt.Sprintf(format, args...)}
}
