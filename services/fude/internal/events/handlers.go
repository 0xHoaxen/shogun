package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
)

// Event types fude consumes.
const (
	TypeJobAdded             = "job.added"
	TypeContactStatusChanged = "contact.status_changed"
)

// Drafts is what the handlers need from the use cases.
type Drafts interface {
	DraftFromEvent(ctx context.Context, tx pgx.Tx, in app.EventDraftInput) error
}

// Handlers returns fude's handlers by event type, for bus.NewSinkServer.
func Handlers(drafts Drafts, log *slog.Logger) map[string]bus.Handler {
	h := &handlers{drafts: drafts, log: log}
	return map[string]bus.Handler{
		TypeJobAdded:             h.jobAdded,
		TypeContactStatusChanged: h.contactStatusChanged,
	}
}

type handlers struct {
	drafts Drafts
	log    *slog.Logger
}

// jobAdded queues a cover letter for a new job. A cover letter has no known
// recipient, so it is copied rather than sent.
func (h *handlers) jobAdded(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p kagamiv1.JobAdded
	if err := env.GetPayload().UnmarshalTo(&p); err != nil {
		return h.drop(env, fmt.Errorf("decode payload: %w", err))
	}
	return h.create(ctx, tx, env, p.GetOwnerId(), p.GetJobId(), app.EventDraftInput{
		Kind: domain.KindCoverLetter, TargetType: domain.TargetJob, Channel: domain.ChannelOther,
	})
}

// contactStatusChanged queues an outreach draft for a contact that moved to a
// new status, on the contact's preferred channel.
func (h *handlers) contactStatusChanged(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p kagamiv1.ContactStatusChanged
	if err := env.GetPayload().UnmarshalTo(&p); err != nil {
		return h.drop(env, fmt.Errorf("decode payload: %w", err))
	}
	return h.create(ctx, tx, env, p.GetOwnerId(), p.GetContactId(), app.EventDraftInput{
		Kind: domain.KindOutreach, TargetType: domain.TargetContact, Channel: channelFor(p.GetChannel()),
	})
}

func (h *handlers) create(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope, ownerID, targetID string, in app.EventDraftInput) error {
	owner, err := uuid.Parse(ownerID)
	if err != nil {
		return h.drop(env, errors.New("owner_id is not a uuid"))
	}
	target, err := uuid.Parse(targetID)
	if err != nil {
		return h.drop(env, errors.New("target id is not a uuid"))
	}
	in.OwnerID, in.TargetID, in.EventID = owner, target, env.GetId()
	return h.drafts.DraftFromEvent(ctx, tx, in)
}

// drop acknowledges an event that can never be handled, so its producer does
// not retry it for a day. It is logged as an error with ids only, never the
// payload.
func (h *handlers) drop(env *eventsv1.Envelope, reason error) error {
	h.log.Error("event dropped",
		slog.String("event_id", env.GetId()), slog.String("event_type", env.GetType()), slog.Any("reason", reason))
	return nil
}

// channelFor maps a contact's preferred channel to a draft channel. Phone and
// anything unknown are copied by hand like the other copy-only channels.
func channelFor(preferred string) domain.Channel {
	if c := domain.Channel(preferred); c.Valid() {
		return c
	}
	return domain.ChannelOther
}
