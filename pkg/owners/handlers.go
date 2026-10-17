package owners

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	toriiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/torii/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
)

// Event types torii produces about users.
const (
	TypeUserActivated = "user.activated"
	TypeUserUpdated   = "user.updated"
	TypeUserDeleted   = "user.deleted"
)

// WithHandlers returns own plus the handlers that keep the owners table up to
// date. own is not modified, and may be nil. A nil logger discards.
func WithHandlers(own map[string]bus.Handler, log *slog.Logger) map[string]bus.Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h := &handlers{log: log}
	all := maps.Clone(own)
	if all == nil {
		all = map[string]bus.Handler{}
	}
	all[TypeUserActivated] = h.activated
	all[TypeUserUpdated] = h.updated
	all[TypeUserDeleted] = h.deleted
	return all
}

type handlers struct {
	log *slog.Logger
}

func (h *handlers) activated(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p toriiv1.UserActivated
	if !h.decode(env, &p) {
		return nil
	}
	return h.store(ctx, tx, env, p.GetOwnerId(), p.GetTimezone(), p.GetStatus(), p.GetVersion())
}

func (h *handlers) updated(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p toriiv1.UserUpdated
	if !h.decode(env, &p) {
		return nil
	}
	return h.store(ctx, tx, env, p.GetOwnerId(), p.GetTimezone(), p.GetStatus(), p.GetVersion())
}

func (h *handlers) deleted(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p toriiv1.UserDeleted
	if !h.decode(env, &p) {
		return nil
	}
	return h.store(ctx, tx, env, p.GetOwnerId(), DefaultTimezone, StatusDeleted, p.GetVersion())
}

// store writes the owner. An event it cannot make sense of is logged and
// acknowledged: retrying would fail the same way for ever.
func (h *handlers) store(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope, ownerID, timezone, status string, version int32) error {
	id, err := uuid.Parse(ownerID)
	if err != nil {
		h.drop(env, fmt.Errorf("owner_id is not a uuid"))
		return nil
	}
	o := Owner{ID: id, Timezone: timezone, Status: status, Version: version}
	if err := o.validate(); err != nil {
		h.drop(env, err)
		return nil
	}
	return Upsert(ctx, tx, o)
}

func (h *handlers) decode(env *eventsv1.Envelope, into proto.Message) bool {
	if err := env.GetPayload().UnmarshalTo(into); err != nil {
		h.drop(env, fmt.Errorf("decode payload: %w", err))
		return false
	}
	return true
}

func (h *handlers) drop(env *eventsv1.Envelope, err error) {
	h.log.Warn("dropping a user event",
		slog.String("event_id", env.GetId()), slog.String("type", env.GetType()), slog.Any("error", err))
}
