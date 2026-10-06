package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/fude/internal/store"
)

// RecordSent marks a draft sent, once tsubame reports its mail went out, inside
// tx (the event's inbox transaction). It is idempotent, and a report for a
// draft that is gone, or for a version the draft has moved past, is ignored.
func (s *Service) RecordSent(ctx context.Context, tx pgx.Tx, owner, draftID uuid.UUID, version int32) error {
	repo := store.New(tx)
	d, err := repo.GetDraft(ctx, owner, draftID)
	if err != nil {
		return ignoreMissing(s.log, err, draftID)
	}
	next, changed, err := toDomain(d).RecordSent(version, s.now())
	if err != nil {
		// A draft discarded or failed before its mail was reported out: the mail
		// is out, but the draft cannot say so. Say so in the log instead.
		s.log.Error("mail was sent for a draft that cannot record it",
			slog.String("draft_id", draftID.String()), slog.String("state", d.State), slog.Any("error", err))
		return nil
	}
	if !changed {
		return nil
	}
	if d.State == "pending" {
		s.log.Warn("recorded a send for a draft that had been put back to pending", slog.String("draft_id", draftID.String()))
	}
	_, err = saveState(ctx, repo, owner, d, next, d.Version)
	return err
}

// RecordSendFailed puts an approved draft back to pending, once tsubame reports
// its mail did not go out, inside tx. A stale report is ignored.
func (s *Service) RecordSendFailed(ctx context.Context, tx pgx.Tx, owner, draftID uuid.UUID, version int32) error {
	_, err := recordSendFailed(ctx, store.New(tx), s.now(), owner, draftID, version)
	return ignoreMissing(s.log, err, draftID)
}

// recordSendFailed does the write for RecordSendFailed and Approver.revert. It
// reports whether the draft changed.
func recordSendFailed(ctx context.Context, repo *store.Repo, now time.Time, owner, draftID uuid.UUID, version int32) (bool, error) {
	d, err := repo.GetDraft(ctx, owner, draftID)
	if err != nil {
		return false, err
	}
	next, changed, err := toDomain(d).RecordSendFailed(version, now)
	if err != nil || !changed {
		return false, err
	}
	_, err = saveState(ctx, repo, owner, d, next, d.Version)
	return err == nil, err
}

// ignoreMissing turns a draft that no longer exists into nothing: there is
// nothing to update, and retrying the event cannot help.
func ignoreMissing(log *slog.Logger, err error, draftID uuid.UUID) error {
	if err == nil || !isNotFound(err) {
		return err
	}
	log.Warn("outcome for a draft that is gone", slog.String("draft_id", draftID.String()))
	return nil
}
