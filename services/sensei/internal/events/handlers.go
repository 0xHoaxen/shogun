// Package events holds sensei's inbox handlers: each turns one event into one
// fact.
package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
)

// Recorder stores the fact an event caused.
type Recorder interface {
	RecordFact(ctx context.Context, tx pgx.Tx, f domain.Fact) error
}

// ownerEvent is a payload that names the owner it is about.
type ownerEvent interface {
	proto.Message
	GetOwnerId() string
}

// projection reads a payload into the dimension of its fact. The payload is
// already decoded into the message it returns.
type projection struct {
	new   func() ownerEvent
	build func(p ownerEvent) map[string]string
}

var projections = map[string]projection{
	domain.TypeJobAdded: {func() ownerEvent { return &kagamiv1.JobAdded{} }, func(p ownerEvent) map[string]string {
		return map[string]string{domain.DimSource: p.(*kagamiv1.JobAdded).GetSource()}
	}},
	domain.TypeJobStatusChanged: {func() ownerEvent { return &kagamiv1.JobStatusChanged{} }, func(p ownerEvent) map[string]string {
		e := p.(*kagamiv1.JobStatusChanged)
		return map[string]string{
			domain.DimFrom: domain.EnumLabel(e.GetFrom().String(), "JOB_STATUS_"),
			domain.DimTo:   domain.EnumLabel(e.GetTo().String(), "JOB_STATUS_"),
		}
	}},
	domain.TypeJobFollowUpDue: {func() ownerEvent { return &kagamiv1.JobFollowUpDue{} }, func(ownerEvent) map[string]string { return nil }},
	domain.TypeContactAdded: {func() ownerEvent { return &kagamiv1.ContactAdded{} }, func(p ownerEvent) map[string]string {
		return map[string]string{domain.DimStatus: domain.EnumLabel(p.(*kagamiv1.ContactAdded).GetStatus().String(), "CONTACT_STATUS_")}
	}},
	domain.TypeContactStatus: {func() ownerEvent { return &kagamiv1.ContactStatusChanged{} }, func(p ownerEvent) map[string]string {
		e := p.(*kagamiv1.ContactStatusChanged)
		return map[string]string{
			domain.DimFrom:    domain.EnumLabel(e.GetFrom().String(), "CONTACT_STATUS_"),
			domain.DimTo:      domain.EnumLabel(e.GetTo().String(), "CONTACT_STATUS_"),
			domain.DimChannel: e.GetChannel(),
		}
	}},
	domain.TypeContactFollowUpDue: {func() ownerEvent { return &kagamiv1.ContactFollowUpDue{} }, func(ownerEvent) map[string]string { return nil }},
	domain.TypeMailClassified: {func() ownerEvent { return &tsubamev1.MailClassified{} }, func(p ownerEvent) map[string]string {
		e := p.(*tsubamev1.MailClassified)
		return map[string]string{
			domain.DimClassification: domain.EnumLabel(e.GetClassification().String(), "MAIL_CLASS_"),
			domain.DimLinked:         linked(e.GetJobId() != "", e.GetContactId() != ""),
		}
	}},
	domain.TypeMailReplyDetected: {func() ownerEvent { return &tsubamev1.MailReplyDetected{} }, func(ownerEvent) map[string]string { return nil }},
	domain.TypeDraftApproved: {func() ownerEvent { return &fudev1.DraftApproved{} }, func(p ownerEvent) map[string]string {
		e := p.(*fudev1.DraftApproved)
		return map[string]string{domain.DimChannel: domain.EnumLabel(e.GetChannel().String(), "CHANNEL_"), domain.DimDraft: e.GetDraftId()}
	}},
	domain.TypeDraftSent: {func() ownerEvent { return &tsubamev1.DraftSent{} }, func(p ownerEvent) map[string]string {
		// Only tsubame sends, and it sends email.
		return map[string]string{domain.DimChannel: "email", domain.DimDraft: p.(*tsubamev1.DraftSent).GetDraftId()}
	}},
	domain.TypeCostThreshold: {func() ownerEvent { return &sorobanv1.CostThresholdReached{} }, func(p ownerEvent) map[string]string {
		e := p.(*sorobanv1.CostThresholdReached)
		scope := domain.EnumLabel(e.GetScopeType().String(), "SCOPE_TYPE_")
		if e.GetScopeValue() != "" {
			scope += ":" + e.GetScopeValue()
		}
		return map[string]string{
			domain.DimScope:   scope,
			domain.DimPercent: fmt.Sprint(e.GetPercent()),
			domain.DimPeriod:  domain.EnumLabel(e.GetPeriod().String(), "BUDGET_PERIOD_"),
		}
	}},
}

func linked(job, contact bool) string {
	switch {
	case job && contact:
		return "both"
	case job:
		return "job"
	case contact:
		return "contact"
	default:
		return "none"
	}
}

// Handlers returns sensei's handlers by event type, for bus.NewSinkServer. now
// stands in for an envelope that carries no time.
func Handlers(rec Recorder, log *slog.Logger, now func() time.Time) map[string]bus.Handler {
	if now == nil {
		now = time.Now
	}
	h := &handlers{rec: rec, log: log, now: now}
	out := make(map[string]bus.Handler, len(projections))
	for typ, p := range projections {
		out[typ] = h.handler(typ, p)
	}
	return out
}

// Types lists the event types sensei consumes.
func Types() []string {
	out := make([]string, 0, len(projections))
	for typ := range projections {
		out = append(out, typ)
	}
	return out
}

type handlers struct {
	rec Recorder
	log *slog.Logger
	now func() time.Time
}

func (h *handlers) handler(typ string, p projection) bus.Handler {
	return func(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
		payload := p.new()
		if err := env.GetPayload().UnmarshalTo(payload); err != nil {
			return h.drop(env, fmt.Errorf("decode payload: %w", err))
		}
		eventID, err := uuid.Parse(env.GetId())
		if err != nil {
			return h.drop(env, errors.New("event id is not a uuid"))
		}
		owner, err := uuid.Parse(payload.GetOwnerId())
		if err != nil {
			return h.drop(env, errors.New("owner_id is not a uuid"))
		}
		at := h.now()
		if env.GetOccurredAt() != nil {
			at = env.GetOccurredAt().AsTime()
		}
		return h.rec.RecordFact(ctx, tx, domain.Fact{
			EventID: eventID, OwnerID: owner, Type: typ, Dimension: p.build(payload), OccurredAt: at,
		})
	}
}

// drop acknowledges an event that can never be handled, so its producer does
// not retry it for a day. It is logged with ids only, never the payload.
func (h *handlers) drop(env *eventsv1.Envelope, reason error) error {
	h.log.Error("event dropped",
		slog.String("event_id", env.GetId()), slog.String("event_type", env.GetType()), slog.Any("reason", reason))
	return nil
}
