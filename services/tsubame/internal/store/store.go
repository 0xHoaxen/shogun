package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

// Errors the app layer maps to outcomes.
var (
	// ErrNotFound means no row matches, or it belongs to another owner.
	ErrNotFound = errors.New("store: not found")
	// ErrDuplicate means a unique value is already taken.
	ErrDuplicate = errors.New("store: duplicate")
)

// uniqueViolation is the Postgres SQLSTATE for a unique index violation.
const uniqueViolation = "23505"

// DBTX is a pool or a transaction.
type DBTX = db.DBTX

// Repo reads and writes the tsubame tables. It holds no state besides the
// connection it was built on.
type Repo struct {
	q *db.Queries
}

// New returns a Repo that runs on d.
func New(d DBTX) *Repo { return &Repo{q: db.New(d)} }

// NewID returns a new UUIDv7.
func NewID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return fmt.Errorf("%w: %s", ErrDuplicate, pgErr.ConstraintName)
	}
	return err
}

// UpsertAccount stores a connected account. An address connected before keeps
// its id and sync cursor and gets the new token.
func (r *Repo) UpsertAccount(ctx context.Context, arg db.UpsertAccountParams) (db.Account, error) {
	a, err := r.q.UpsertAccount(ctx, arg)
	if err != nil {
		return db.Account{}, fmt.Errorf("upsert account: %w", mapErr(err))
	}
	return a, nil
}

// GetAccount returns one account of the owner.
func (r *Repo) GetAccount(ctx context.Context, owner, id uuid.UUID) (db.Account, error) {
	a, err := r.q.GetAccount(ctx, db.GetAccountParams{ID: id, OwnerID: owner})
	return a, mapErr(err)
}

// ListAccounts returns the owner's accounts, oldest first.
func (r *Repo) ListAccounts(ctx context.Context, owner uuid.UUID) ([]db.Account, error) {
	as, err := r.q.ListAccounts(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", mapErr(err))
	}
	return as, nil
}

// SetAccountStatus changes an account's status. It is ErrNotFound when the
// account is not the owner's.
func (r *Repo) SetAccountStatus(ctx context.Context, owner, id uuid.UUID, status string) error {
	n, err := r.q.SetAccountStatus(ctx, db.SetAccountStatusParams{ID: id, OwnerID: owner, Status: status})
	if err != nil {
		return fmt.Errorf("set account status: %w", mapErr(err))
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListActiveAccounts returns every owner's accounts that can be synced.
func (r *Repo) ListActiveAccounts(ctx context.Context) ([]db.Account, error) {
	as, err := r.q.ListActiveAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active accounts: %w", mapErr(err))
	}
	return as, nil
}

// SetAccountCursor records a finished sync: the new history cursor and when it
// ran.
func (r *Repo) SetAccountCursor(ctx context.Context, id uuid.UUID, historyID string, syncedAt time.Time) error {
	n, err := r.q.SetAccountCursor(ctx, db.SetAccountCursorParams{ID: id, HistoryID: &historyID, SyncedAt: &syncedAt})
	if err != nil {
		return fmt.Errorf("set account cursor: %w", mapErr(err))
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// InsertMessage stores a message. It reports false, and stores nothing, when
// the account already has a message with that provider id.
func (r *Repo) InsertMessage(ctx context.Context, arg db.InsertMessageParams) (db.Message, bool, error) {
	m, err := r.q.InsertMessage(ctx, arg)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Message{}, false, nil
	}
	if err != nil {
		return db.Message{}, false, fmt.Errorf("insert message: %w", err)
	}
	return m, true, nil
}

// GetMessage returns one message of the owner.
func (r *Repo) GetMessage(ctx context.Context, owner, id uuid.UUID) (db.Message, error) {
	m, err := r.q.GetMessage(ctx, db.GetMessageParams{ID: id, OwnerID: owner})
	return m, mapErr(err)
}

// ClassifyArgs is what a classification stores on a message.
type ClassifyArgs struct {
	Classification string
	Confidence     float32
	ClassifiedBy   string
	JobID          *uuid.UUID
	ContactID      *uuid.UUID
}

// SetMessageClassification classifies a message that is not classified yet. It
// reports false, and changes nothing, when it already was or is not the
// owner's.
func (r *Repo) SetMessageClassification(ctx context.Context, owner, id uuid.UUID, a ClassifyArgs) (bool, error) {
	n, err := r.q.SetMessageClassification(ctx, db.SetMessageClassificationParams{
		ID: id, OwnerID: owner, Classification: &a.Classification, Confidence: &a.Confidence,
		ClassifiedBy: &a.ClassifiedBy, LinkedJobID: a.JobID, LinkedContactID: a.ContactID,
	})
	if err != nil {
		return false, fmt.Errorf("classify message: %w", err)
	}
	return n > 0, nil
}

// ThreadHasOutbound reports whether the owner sent a message in the thread.
func (r *Repo) ThreadHasOutbound(ctx context.Context, accountID uuid.UUID, threadID string) (bool, error) {
	has, err := r.q.ThreadHasOutbound(ctx, db.ThreadHasOutboundParams{AccountID: accountID, ThreadID: &threadID})
	if err != nil {
		return false, fmt.Errorf("thread has outbound: %w", err)
	}
	return has, nil
}

// ThreadLinks returns the job and contact an earlier message of the thread was
// linked to, either of which may be nil. Both are nil when none was.
func (r *Repo) ThreadLinks(ctx context.Context, accountID, messageID uuid.UUID, threadID string) (job, contact *uuid.UUID, err error) {
	row, err := r.q.ThreadLinks(ctx, db.ThreadLinksParams{AccountID: accountID, ThreadID: &threadID, ID: messageID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("thread links: %w", err)
	}
	return row.LinkedJobID, row.LinkedContactID, nil
}

// InsertSend records that a send was started. A token id used before is
// ErrDuplicate: that Hanko was already spent.
func (r *Repo) InsertSend(ctx context.Context, arg db.InsertSendParams) (db.Send, error) {
	snd, err := r.q.InsertSend(ctx, arg)
	if err != nil {
		return db.Send{}, fmt.Errorf("insert send: %w", mapErr(err))
	}
	return snd, nil
}

// MarkSendSent records that the provider accepted the mail. It reports false
// when the send was no longer in flight.
func (r *Repo) MarkSendSent(ctx context.Context, id uuid.UUID, providerMessageID string, sentAt time.Time) (bool, error) {
	n, err := r.q.MarkSendSent(ctx, db.MarkSendSentParams{ID: id, ProviderMessageID: &providerMessageID, SentAt: &sentAt})
	if err != nil {
		return false, fmt.Errorf("mark send sent: %w", err)
	}
	return n > 0, nil
}

// MarkSendFailed records that the mail did not go out, with a short reason. It
// reports false when the send was no longer in flight.
func (r *Repo) MarkSendFailed(ctx context.Context, id uuid.UUID, reason string) (bool, error) {
	n, err := r.q.MarkSendFailed(ctx, db.MarkSendFailedParams{ID: id, Error: &reason})
	if err != nil {
		return false, fmt.Errorf("mark send failed: %w", err)
	}
	return n > 0, nil
}

// ListOpenSends returns up to limit sends still in flight that started at or
// before startedBefore, oldest first.
func (r *Repo) ListOpenSends(ctx context.Context, startedBefore time.Time, limit int32) ([]db.Send, error) {
	rows, err := r.q.ListOpenSends(ctx, db.ListOpenSendsParams{StartedBefore: startedBefore, RowLimit: limit})
	if err != nil {
		return nil, fmt.Errorf("list open sends: %w", err)
	}
	return rows, nil
}
