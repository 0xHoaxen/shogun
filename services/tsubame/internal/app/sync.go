package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

const (
	// fullSyncQuery picks the mail a full sync reads: a month, which is as far
	// back as a job search needs.
	fullSyncQuery = "newer_than:30d"
	// fullSyncMax caps one full sync, so a huge mailbox cannot run away.
	fullSyncMax = 500
	// listPageSize is how many ids a full sync asks for at a time.
	listPageSize = 100
)

// ClassifyQueue starts classification of a stored message inside the sync's
// transaction, so a message and its job succeed or fail together.
type ClassifyQueue interface {
	EnqueueClassify(ctx context.Context, tx pgx.Tx, owner, messageID uuid.UUID) error
}

// SyncResult is what one account's sync did.
type SyncResult struct {
	// Added is how many new messages were stored.
	Added int
	// Full is true when the sync read the last month instead of a history
	// cursor, because it had no cursor yet or Gmail had dropped it.
	Full bool
}

// Syncer copies new mail from the providers into tsubame.
type Syncer struct {
	pool     *pgxpool.Pool
	accounts *Accounts
	factory  mail.Factory
	queue    ClassifyQueue
	log      *slog.Logger
	now      func() time.Time
}

// NewSyncer returns a Syncer. A nil queue stores messages without classifying
// them; a nil now means time.Now.
func NewSyncer(pool *pgxpool.Pool, accounts *Accounts, factory mail.Factory, queue ClassifyQueue, log *slog.Logger, now func() time.Time) *Syncer {
	if now == nil {
		now = time.Now
	}
	return &Syncer{pool: pool, accounts: accounts, factory: factory, queue: queue, log: log, now: now}
}

// SyncAll syncs every active account. One account failing does not stop the
// others; it is logged, and counted in the returned error.
func (s *Syncer) SyncAll(ctx context.Context) error {
	accounts, err := store.New(s.pool).ListActiveAccounts(ctx)
	if err != nil {
		return err
	}
	var failed []error
	for _, acc := range accounts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		res, err := s.SyncAccount(ctx, acc)
		if err != nil {
			s.log.Warn("mail sync failed", slog.String("account_id", acc.ID.String()), slog.Any("error", err))
			failed = append(failed, err)
			continue
		}
		if res.Added > 0 || res.Full {
			s.log.Info("mail synced", slog.String("account_id", acc.ID.String()),
				slog.Int("added", res.Added), slog.Bool("full", res.Full))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d accounts failed to sync", len(failed), len(accounts))
	}
	return nil
}

// SyncAccount reads what is new in one account. The history cursor moves only
// after every new message has been fetched and stored, so a failure part way
// leaves the cursor where it was and the next run reads the same messages again.
func (s *Syncer) SyncAccount(ctx context.Context, acc db.Account) (SyncResult, error) {
	provider, err := s.accounts.Provider(ctx, acc.OwnerID, acc.ID, s.factory, s.log)
	if err != nil {
		return SyncResult{}, err
	}
	ids, cursor, full, err := s.discover(ctx, provider, acc)
	if err != nil {
		return SyncResult{}, err
	}
	msgs, err := fetchAll(ctx, provider, ids)
	if err != nil {
		return SyncResult{}, err
	}
	added, err := s.store(ctx, acc, msgs, cursor)
	return SyncResult{Added: added, Full: full}, err
}

// discover returns the ids to read and the cursor to store once they are read.
func (s *Syncer) discover(ctx context.Context, p mail.Provider, acc db.Account) (ids []string, cursor string, full bool, err error) {
	if acc.HistoryID != nil && *acc.HistoryID != "" {
		h, err := p.History(ctx, *acc.HistoryID)
		if err == nil {
			return h.AddedIDs, nonEmpty(h.LatestID, *acc.HistoryID), false, nil
		}
		if !errors.Is(err, mail.ErrHistoryExpired) {
			return nil, "", false, err
		}
		s.log.Info("history cursor expired, reading the last month", slog.String("account_id", acc.ID.String()))
	}
	// The cursor is taken before listing, so mail that arrives during the
	// listing is read by the next incremental sync instead of being missed.
	profile, err := p.Profile(ctx)
	if err != nil {
		return nil, "", true, err
	}
	ids, err = listRecent(ctx, p)
	return ids, profile.HistoryID, true, err
}

func listRecent(ctx context.Context, p mail.Provider) ([]string, error) {
	var ids []string
	token := ""
	for len(ids) < fullSyncMax {
		page, err := p.List(ctx, mail.ListQuery{Query: fullSyncQuery, PageToken: token, Max: listPageSize})
		if err != nil {
			return nil, err
		}
		ids = append(ids, page.IDs...)
		if page.NextPageToken == "" {
			break
		}
		token = page.NextPageToken
	}
	return ids[:min(len(ids), fullSyncMax)], nil
}

// fetchAll reads every message. One deleted since it was listed is skipped; any
// other failure stops the sync.
func fetchAll(ctx context.Context, p mail.Provider, ids []string) ([]mail.Message, error) {
	msgs := make([]mail.Message, 0, len(ids))
	for _, id := range ids {
		m, err := p.Get(ctx, id)
		if errors.Is(err, mail.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("get message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// store saves the messages, queues classification of the inbound new ones and
// moves the cursor, in one transaction.
func (s *Syncer) store(ctx context.Context, acc db.Account, msgs []mail.Message, cursor string) (int, error) {
	added := 0
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		repo := store.New(tx)
		added = 0
		for _, m := range msgs {
			row, inserted, err := repo.InsertMessage(ctx, messageParams(acc, m))
			if err != nil {
				return err
			}
			if !inserted {
				continue
			}
			added++
			if m.Direction == mail.Inbound && s.queue != nil {
				if err := s.queue.EnqueueClassify(ctx, tx, acc.OwnerID, row.ID); err != nil {
					return err
				}
			}
		}
		return repo.SetAccountCursor(ctx, acc.ID, cursor, s.now())
	})
	return added, err
}

func messageParams(acc db.Account, m mail.Message) db.InsertMessageParams {
	return db.InsertMessageParams{
		ID: store.NewID(), OwnerID: acc.OwnerID, AccountID: acc.ID, ProviderMessageID: m.ID,
		ProviderThreadID: strPtr(m.ThreadID), Direction: string(m.Direction), FromAddr: m.From,
		ToAddrs: nonNilStrings(m.To), Subject: strPtr(m.Subject), Snippet: strPtr(m.Snippet), ReceivedAt: m.ReceivedAt,
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
