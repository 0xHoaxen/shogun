package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
)

const (
	// AutoApplyConfidence is the least confidence at which mail moves a job or
	// contact by itself. Below it the mail is only a suggestion, which taiko
	// raises as a notification.
	AutoApplyConfidence = 0.9

	jobEventMailLinked       = "mail_linked"
	contactEventMessageSent  = "message_sent"
	contactEventReplyReceive = "reply_received"

	// sourceMail marks a timeline entry made from a mail event.
	sourceMail = "mail"
)

// mailJobTargets is the job status each class of mail points to. Mail that is
// not about a job's progress (a reply, a recruiter's note, anything else) moves
// nothing.
var mailJobTargets = map[string]domain.JobStatus{
	"application_confirmation": domain.JobApplied,
	"interview_invite":         domain.JobInterview,
	"rejection":                domain.JobRejected,
	"offer":                    domain.JobOffer,
}

// MailClassified is what kagami needs from a mail.classified event.
type MailClassified struct {
	Owner      uuid.UUID
	MessageID  string
	Class      string
	Confidence float32
	JobID      *uuid.UUID
	// OccurredAt is when the event happened, not when it arrived.
	OccurredAt time.Time
}

// ApplyMailClassified records that a message was linked to a job and, when the
// mail is sure enough and says the job moved, moves it, inside tx (the event's
// inbox transaction).
//
// Mail is only trusted to move a job forward as the state machine allows: it
// never reopens a rejected job (only the owner does), never repeats the status
// the job is already in, and an event that occurred before the job's last status
// change is ignored as out of date, so events arriving out of order cannot undo
// a newer one.
func (s *Service) ApplyMailClassified(ctx context.Context, tx pgx.Tx, in MailClassified) error {
	if in.JobID == nil {
		return nil
	}
	repo := store.New(tx)
	job, err := repo.GetJob(ctx, in.Owner, *in.JobID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // the job is gone; nothing to link or move
	}
	if err != nil {
		return err
	}
	if err := s.addJobEvent(ctx, repo, job.ID, jobEventMailLinked, nil, nil, map[string]any{
		"message_id": in.MessageID, "classification": in.Class, "confidence": in.Confidence,
	}); err != nil {
		return err
	}

	target, moves := mailJobTargets[in.Class]
	if !moves || in.Confidence < AutoApplyConfidence {
		return nil
	}
	current := domain.JobStatus(job.Status)
	if current == domain.JobRejected || current == target || !current.CanMoveTo(target) {
		return nil
	}
	stale, err := s.staleJobEvent(ctx, repo, job.ID, in.OccurredAt)
	if err != nil || stale {
		return err
	}
	_, err = s.moveJob(ctx, tx, repo, in.Owner, job, target, "", map[string]any{
		"source": sourceMail, "message_id": in.MessageID, "confidence": in.Confidence, "event_at": eventTime(in.OccurredAt),
	})
	return err
}

// ApplyReplyDetected moves a contact who wrote back to replied, inside tx, when
// the state machine allows it. A contact not yet reached out to is left alone: a
// reply to mail kagami never recorded sending cannot be placed in the sequence.
func (s *Service) ApplyReplyDetected(ctx context.Context, tx pgx.Tx, owner, contactID uuid.UUID, messageID string, occurredAt time.Time) error {
	repo := store.New(tx)
	contact, err := repo.GetContact(ctx, owner, contactID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.addContactEvent(ctx, repo, contact.ID, contactEventReplyReceive, nil, nil, nil, map[string]any{"message_id": messageID}); err != nil {
		return err
	}
	current := domain.ContactStatus(contact.Status)
	if current == domain.ContactReplied || !current.CanMoveTo(domain.ContactReplied) {
		return nil
	}
	stale, err := s.staleContactEvent(ctx, repo, contact.ID, occurredAt)
	if err != nil || stale {
		return err
	}
	_, err = s.moveContact(ctx, tx, repo, owner, contact, domain.ContactReplied, contact.LastContacted, map[string]any{
		"source": sourceMail, "message_id": messageID, "event_at": eventTime(occurredAt),
	})
	return err
}

// DraftSent is what kagami needs from a draft.sent event.
type DraftSent struct {
	Owner     uuid.UUID
	ContactID *uuid.UUID
	DraftID   string
	SentAt    time.Time
}

// ApplyDraftSent records that mail went out to a contact, inside tx: a contact
// not yet reached moves to reached_out, and last_contacted moves forward to the
// day it was sent, never back.
func (s *Service) ApplyDraftSent(ctx context.Context, tx pgx.Tx, in DraftSent) error {
	if in.ContactID == nil {
		return nil
	}
	repo := store.New(tx)
	contact, err := repo.GetContact(ctx, in.Owner, *in.ContactID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	day := time.Date(in.SentAt.UTC().Year(), in.SentAt.UTC().Month(), in.SentAt.UTC().Day(), 0, 0, 0, 0, time.UTC)
	last, advanced := contact.LastContacted, false
	if last == nil || day.After(*last) {
		last, advanced = &day, true
	}

	current := domain.ContactStatus(contact.Status)
	switch {
	case current == domain.ContactNotReached:
		if _, err := s.moveContact(ctx, tx, repo, in.Owner, contact, domain.ContactReachedOut, last, map[string]any{
			"source": sourceMail, "draft_id": in.DraftID, "event_at": eventTime(in.SentAt),
		}); err != nil {
			return err
		}
	case advanced:
		if _, err := repo.UpdateContactLastContacted(ctx, in.Owner, contact, last); err != nil {
			return err
		}
	}
	return s.addContactEvent(ctx, repo, contact.ID, contactEventMessageSent, nil, nil, nil, map[string]any{"draft_id": in.DraftID})
}

// staleJobEvent reports whether an event that occurred at when is older than the
// job's latest status change.
func (s *Service) staleJobEvent(ctx context.Context, repo *store.Repo, jobID uuid.UUID, when time.Time) (bool, error) {
	anchor, found, err := repo.LatestJobStatusAnchor(ctx, jobID)
	return found && when.Before(anchor), err
}

// staleContactEvent is staleJobEvent for a contact.
func (s *Service) staleContactEvent(ctx context.Context, repo *store.Repo, contactID uuid.UUID, when time.Time) (bool, error) {
	anchor, found, err := repo.LatestContactStatusAnchor(ctx, contactID)
	return found && when.Before(anchor), err
}

// eventTime formats an event time for a timeline payload.
func eventTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
