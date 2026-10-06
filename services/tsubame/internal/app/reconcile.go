package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

const (
	// reconcileGrace leaves a send alone while its own call may still be
	// finishing.
	reconcileGrace = time.Minute
	// reconcileGiveUp is how long a send may stay open before it is called
	// failed when its mail is not in the provider's Sent folder.
	reconcileGiveUp = 10 * time.Minute
	// reconcileBatch bounds one run.
	reconcileBatch = 50
	// sentQuery and sentPageSize pick the recent sent mail a run looks through.
	sentQuery    = "in:sent newer_than:2d"
	sentPageSize = 50
)

// ReconcileResult is what one run did.
type ReconcileResult struct {
	// Recovered sends were found in Sent and recorded as sent.
	Recovered int
	// Failed sends were given up on and recorded as failed.
	Failed int
}

// Reconcile resolves sends left in "sending", which happens when tsubame stops
// between recording a send and recording the provider's answer. It looks for the
// X-Shogun-Draft header in the provider's Sent mail. Found: the mail went out,
// so it is recorded as sent and draft.sent is emitted. Not found after ten
// minutes: it is recorded as failed and draft.send_failed is emitted, so the
// draft can be approved again. A younger row, or one the provider cannot be
// asked about, is left for the next run. It never sends anything.
func (s *Sender) Reconcile(ctx context.Context) (ReconcileResult, error) {
	open, err := store.New(s.pool).ListOpenSends(ctx, s.now().Add(-reconcileGrace), reconcileBatch)
	if err != nil {
		return ReconcileResult{}, err
	}
	var res ReconcileResult
	sentByAccount := map[uuid.UUID]map[string]mail.Message{}
	for _, row := range open {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		sent, ok := s.sentMail(ctx, row, sentByAccount)
		age := s.now().Sub(row.CreatedAt)
		switch {
		case ok && sent[draftHeader(row.DraftID.String(), row.DraftVersion)].ID != "":
			m := sent[draftHeader(row.DraftID.String(), row.DraftVersion)]
			if err := s.succeed(ctx, row, m.ID, m.ReceivedAt); err != nil {
				s.log.Error("record recovered send", slog.String("send_id", row.ID.String()), slog.Any("error", err))
				continue
			}
			res.Recovered++
		case ok && age >= reconcileGiveUp:
			s.fail(ctx, row, ReasonNotInSent)
			res.Failed++
		}
	}
	return res, nil
}

// sentMail returns the recent sent mail of a send's account, keyed by draft
// header, reading each account once per run. ok is false when the provider
// cannot be asked.
func (s *Sender) sentMail(ctx context.Context, row db.Send, cache map[uuid.UUID]map[string]mail.Message) (map[string]mail.Message, bool) {
	if sent, done := cache[row.AccountID]; done {
		return sent, sent != nil
	}
	sent, err := s.readSent(ctx, row)
	if err != nil {
		s.log.Warn("cannot read sent mail to reconcile", slog.String("account_id", row.AccountID.String()), slog.Any("error", err))
	}
	cache[row.AccountID] = sent // nil when unreadable, so the account is not asked again this run
	return sent, sent != nil
}

func (s *Sender) readSent(ctx context.Context, row db.Send) (map[string]mail.Message, error) {
	provider, err := s.accounts.Provider(ctx, row.OwnerID, row.AccountID, s.factory, s.log)
	if err != nil {
		return nil, err
	}
	page, err := provider.List(ctx, mail.ListQuery{Query: sentQuery, Max: sentPageSize})
	if err != nil {
		return nil, err
	}
	byHeader := map[string]mail.Message{}
	for _, id := range page.IDs {
		m, err := provider.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get sent message: %w", err)
		}
		if m.DraftID != "" {
			byHeader[m.DraftID] = m
		}
	}
	return byHeader, nil
}
