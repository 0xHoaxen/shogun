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

// Purge removes every row of one owner from the service's own schema. It runs
// in the transaction that records the deletion, and must be idempotent.
type Purge func(ctx context.Context, tx pgx.Tx, owner uuid.UUID) error

// Option customises WithHandlers.
type Option func(*handlers)

// WithPurge runs purge when a user is deleted, in the same transaction as the
// tombstone, so the data goes exactly when the deletion is recorded. It runs
// once per deletion: a repeated or stale user.deleted does not run it, so it
// cannot wipe an account the user has since made again. If purge fails the
// whole event fails and is retried.
func WithPurge(purge Purge) Option { return func(h *handlers) { h.purge = purge } }

// WithHandlers returns own plus the handlers that keep the owners table up to
// date. own is not modified, and may be nil. A nil logger discards.
func WithHandlers(own map[string]bus.Handler, log *slog.Logger, opts ...Option) map[string]bus.Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h := &handlers{log: log}
	for _, opt := range opts {
		opt(h)
	}
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
	log   *slog.Logger
	purge Purge
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
	applied, id, err := h.storeApplied(ctx, tx, env, p.GetOwnerId(), DefaultTimezone, StatusDeleted, p.GetVersion())
	if err != nil || !applied || h.purge == nil {
		return err
	}
	if err := h.purge(ctx, tx, id); err != nil {
		return fmt.Errorf("owners: purge %s: %w", id, err)
	}
	return nil
}

// store writes the owner. An event it cannot make sense of is logged and
// acknowledged: retrying would fail the same way for ever.
func (h *handlers) store(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope, ownerID, timezone, status string, version int32) error {
	_, _, err := h.storeApplied(ctx, tx, env, ownerID, timezone, status, version)
	return err
}

// storeApplied is store that also reports whether the event changed the table,
// and the owner it was about.
func (h *handlers) storeApplied(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope, ownerID, timezone, status string, version int32) (bool, uuid.UUID, error) {
	id, err := uuid.Parse(ownerID)
	if err != nil {
		h.drop(env, fmt.Errorf("owner_id is not a uuid"))
		return false, uuid.Nil, nil
	}
	o := Owner{ID: id, Timezone: timezone, Status: status, Version: version}
	if err := o.validate(); err != nil {
		h.drop(env, err)
		return false, uuid.Nil, nil
	}
	applied, err := upsert(ctx, tx, o)
	return applied, id, err
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
