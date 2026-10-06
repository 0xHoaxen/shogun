package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
)

// Event types kagami consumes.
const (
	TypeMailClassified    = "mail.classified"
	TypeMailReplyDetected = "mail.reply_detected"
	TypeDraftSent         = "draft.sent"

	mailClassPrefix = "MAIL_CLASS_"
)

// Mail is what the handlers need from the use cases.
type Mail interface {
	ApplyMailClassified(ctx context.Context, tx pgx.Tx, in app.MailClassified) error
	ApplyReplyDetected(ctx context.Context, tx pgx.Tx, owner, contactID uuid.UUID, messageID string, occurredAt time.Time) error
	ApplyDraftSent(ctx context.Context, tx pgx.Tx, in app.DraftSent) error
}

// Handlers returns kagami's handlers by event type, for bus.NewSinkServer.
func Handlers(mail Mail, log *slog.Logger) map[string]bus.Handler {
	h := &handlers{mail: mail, log: log}
	return map[string]bus.Handler{
		TypeMailClassified:    h.mailClassified,
		TypeMailReplyDetected: h.replyDetected,
		TypeDraftSent:         h.draftSent,
	}
}

type handlers struct {
	mail Mail
	log  *slog.Logger
}

func (h *handlers) mailClassified(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p tsubamev1.MailClassified
	if err := env.GetPayload().UnmarshalTo(&p); err != nil {
		return h.drop(env, fmt.Errorf("decode payload: %w", err))
	}
	owner, err := uuid.Parse(p.GetOwnerId())
	if err != nil {
		return h.drop(env, errors.New("owner_id is not a uuid"))
	}
	job, err := optionalID(p.GetJobId())
	if err != nil {
		return h.drop(env, errors.New("job_id is not a uuid"))
	}
	return h.mail.ApplyMailClassified(ctx, tx, app.MailClassified{
		Owner: owner, MessageID: p.GetMessageId(), Class: strings.ToLower(strings.TrimPrefix(p.GetClassification().String(), mailClassPrefix)),
		Confidence: p.GetConfidence(), JobID: job, OccurredAt: occurredAt(env),
	})
}

func (h *handlers) replyDetected(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p tsubamev1.MailReplyDetected
	if err := env.GetPayload().UnmarshalTo(&p); err != nil {
		return h.drop(env, fmt.Errorf("decode payload: %w", err))
	}
	owner, ownerErr := uuid.Parse(p.GetOwnerId())
	contact, contactErr := uuid.Parse(p.GetContactId())
	if ownerErr != nil || contactErr != nil {
		return h.drop(env, errors.New("owner or contact id is not a uuid"))
	}
	return h.mail.ApplyReplyDetected(ctx, tx, owner, contact, p.GetMessageId(), occurredAt(env))
}

func (h *handlers) draftSent(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p tsubamev1.DraftSent
	if err := env.GetPayload().UnmarshalTo(&p); err != nil {
		return h.drop(env, fmt.Errorf("decode payload: %w", err))
	}
	owner, err := uuid.Parse(p.GetOwnerId())
	if err != nil {
		return h.drop(env, errors.New("owner_id is not a uuid"))
	}
	contact, err := optionalID(p.GetContactId())
	if err != nil {
		return h.drop(env, errors.New("contact_id is not a uuid"))
	}
	sentAt := occurredAt(env)
	if p.GetSentAt() != nil {
		sentAt = p.GetSentAt().AsTime()
	}
	return h.mail.ApplyDraftSent(ctx, tx, app.DraftSent{Owner: owner, ContactID: contact, DraftID: p.GetDraftId(), SentAt: sentAt})
}

// drop acknowledges an event that can never be handled, so its producer does
// not retry it for a day. It is logged as an error with ids only, never the
// payload.
func (h *handlers) drop(env *eventsv1.Envelope, reason error) error {
	h.log.Error("event dropped",
		slog.String("event_id", env.GetId()), slog.String("event_type", env.GetType()), slog.Any("reason", reason))
	return nil
}

// occurredAt is when an event happened, falling back to now for an envelope
// without a time.
func occurredAt(env *eventsv1.Envelope) time.Time {
	if env.GetOccurredAt() == nil {
		return time.Now().UTC()
	}
	return env.GetOccurredAt().AsTime()
}

func optionalID(s string) (*uuid.UUID, error) {
	if s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return &id, nil
}
