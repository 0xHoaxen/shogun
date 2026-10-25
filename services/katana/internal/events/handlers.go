// Package events holds katana's inbox handlers.
package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

const (
	// TypeItemCompleted is the event type of a finished learning item.
	TypeItemCompleted = "learning.item_completed"
	// TypeJobAdded is the event type of a job the owner saved.
	TypeJobAdded = "job.added"
)

const kindPrefix = "ITEM_KIND_"

// Learner is what the handlers need from the use cases.
type Learner interface {
	LearnedItem(ctx context.Context, tx pgx.Tx, owner uuid.UUID, item domain.LearnedItem) error
	JobAdded(ctx context.Context, tx pgx.Tx, owner, job uuid.UUID) error
}

// Handlers returns katana's handlers by event type, for bus.NewSinkServer.
func Handlers(learner Learner, log *slog.Logger) map[string]bus.Handler {
	h := &handlers{learner: learner, log: log}
	return map[string]bus.Handler{TypeItemCompleted: h.itemCompleted, TypeJobAdded: h.jobAdded}
}

type handlers struct {
	learner Learner
	log     *slog.Logger
}

// itemCompleted asks for a suggestion run about a finished item.
func (h *handlers) itemCompleted(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p dojov1.LearningItemCompleted
	if err := env.GetPayload().UnmarshalTo(&p); err != nil {
		return h.drop(env, fmt.Errorf("decode payload: %w", err))
	}
	owner, err := uuid.Parse(p.GetOwnerId())
	if err != nil {
		return h.drop(env, errors.New("owner_id is not a uuid"))
	}
	if _, err := uuid.Parse(p.GetItemId()); err != nil {
		return h.drop(env, errors.New("item_id is not a uuid"))
	}
	kind := ""
	if p.GetKind() != dojov1.ItemKind_ITEM_KIND_UNSPECIFIED {
		kind = lower(p.GetKind().String()[len(kindPrefix):])
	}
	return h.learner.LearnedItem(ctx, tx, owner, domain.LearnedItem{
		ItemID: p.GetItemId(), Title: p.GetTitle(), Kind: kind, URL: p.GetUrl(),
	})
}

// jobAdded asks for a saved job to be read for its skills.
func (h *handlers) jobAdded(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p kagamiv1.JobAdded
	if err := env.GetPayload().UnmarshalTo(&p); err != nil {
		return h.drop(env, fmt.Errorf("decode payload: %w", err))
	}
	owner, err := uuid.Parse(p.GetOwnerId())
	if err != nil {
		return h.drop(env, errors.New("owner_id is not a uuid"))
	}
	job, err := uuid.Parse(p.GetJobId())
	if err != nil {
		return h.drop(env, errors.New("job_id is not a uuid"))
	}
	return h.learner.JobAdded(ctx, tx, owner, job)
}

// drop acknowledges an event that can never be handled, so its producer does
// not retry it for a day. It is logged with ids only, never the payload.
func (h *handlers) drop(env *eventsv1.Envelope, reason error) error {
	h.log.Error("event dropped",
		slog.String("event_id", env.GetId()), slog.String("event_type", env.GetType()), slog.Any("reason", reason))
	return nil
}

func lower(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + ('a' - 'A')
		}
	}
	return string(out)
}
