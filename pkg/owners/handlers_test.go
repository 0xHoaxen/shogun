package owners_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	toriiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/torii/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/owners"
)

func envelope(t *testing.T, typ string, payload proto.Message) *eventsv1.Envelope {
	t.Helper()
	packed, err := anypb.New(payload)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	return &eventsv1.Envelope{Id: uuid.NewString(), Type: typ, Payload: packed}
}

// deliver runs the handler for env in a transaction, as the sink does.
func deliver(ctx context.Context, t *testing.T, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, h bus.Handler, env *eventsv1.Envelope,
) error {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := h(ctx, tx, env); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func TestHandlersKeepTheOwnersTableFromUserEvents(t *testing.T) {
	// Arrange
	ctx, pool := newPool(t)
	handlers := owners.WithHandlers(nil, nil)
	id := uuid.New()

	// Act
	activated := deliver(ctx, t, pool, handlers[owners.TypeUserActivated], envelope(t, owners.TypeUserActivated,
		&toriiv1.UserActivated{OwnerId: id.String(), Timezone: "Asia/Tokyo", Status: "active", Version: 1}))
	created, _ := owners.Get(ctx, pool, id)
	updated := deliver(ctx, t, pool, handlers[owners.TypeUserUpdated], envelope(t, owners.TypeUserUpdated,
		&toriiv1.UserUpdated{OwnerId: id.String(), Timezone: "Europe/Berlin", Status: "disabled", Version: 2}))
	changed, _ := owners.Get(ctx, pool, id)

	// Assert
	if activated != nil || updated != nil {
		t.Fatalf("handlers: %v, %v", activated, updated)
	}
	if created.Timezone != "Asia/Tokyo" || created.Status != owners.StatusActive {
		t.Fatalf("after user.activated: %+v", created)
	}
	if changed.Timezone != "Europe/Berlin" || changed.Status != owners.StatusDisabled || changed.Version != 2 {
		t.Fatalf("after user.updated: %+v", changed)
	}
}

func TestADeletedOwnerIsNotBroughtBackByALateEvent(t *testing.T) {
	// Arrange
	ctx, pool := newPool(t)
	handlers := owners.WithHandlers(nil, nil)
	id := uuid.New()
	if err := deliver(ctx, t, pool, handlers[owners.TypeUserActivated], envelope(t, owners.TypeUserActivated,
		&toriiv1.UserActivated{OwnerId: id.String(), Timezone: "UTC", Status: "active", Version: 1})); err != nil {
		t.Fatalf("activated: %v", err)
	}

	// Act
	deleted := deliver(ctx, t, pool, handlers[owners.TypeUserDeleted], envelope(t, owners.TypeUserDeleted,
		&toriiv1.UserDeleted{OwnerId: id.String(), Version: 3}))
	late := deliver(ctx, t, pool, handlers[owners.TypeUserUpdated], envelope(t, owners.TypeUserUpdated,
		&toriiv1.UserUpdated{OwnerId: id.String(), Timezone: "UTC", Status: "active", Version: 2}))
	active, _ := owners.ListActive(ctx, pool)
	got, _ := owners.Get(ctx, pool, id)

	// Assert
	if deleted != nil || late != nil {
		t.Fatalf("handlers: %v, %v", deleted, late)
	}
	if len(active) != 0 || got.Status != owners.StatusDeleted {
		t.Fatalf("active %+v, owner %+v, want it deleted and not listed", active, got)
	}
}

func TestHandlersIgnoreAMalformedEventInsteadOfRetryingItForever(t *testing.T) {
	ctx, pool := newPool(t)
	handlers := owners.WithHandlers(nil, nil)
	tests := map[string]*eventsv1.Envelope{
		"wrong payload type": envelope(t, owners.TypeUserActivated, wrapperspb.String("x")),
		"bad owner id":       envelope(t, owners.TypeUserActivated, &toriiv1.UserActivated{OwnerId: "nope", Timezone: "UTC", Status: "active", Version: 1}),
		"unknown status":     envelope(t, owners.TypeUserActivated, &toriiv1.UserActivated{OwnerId: uuid.NewString(), Timezone: "UTC", Status: "weird", Version: 1}),
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			if err := deliver(ctx, t, pool, handlers[owners.TypeUserActivated], env); err != nil {
				t.Fatalf("handler returned %v, want a malformed event acknowledged", err)
			}
		})
	}
	if active, _ := owners.ListActive(ctx, pool); len(active) != 0 {
		t.Fatalf("a malformed event created %+v", active)
	}
}

func TestWithHandlersKeepsTheServicesOwnHandlers(t *testing.T) {
	own := func(context.Context, pgx.Tx, *eventsv1.Envelope) error { return nil }

	got := owners.WithHandlers(map[string]bus.Handler{"job.added": own}, nil)

	for _, typ := range []string{"job.added", owners.TypeUserActivated, owners.TypeUserUpdated, owners.TypeUserDeleted} {
		if got[typ] == nil {
			t.Errorf("no handler for %s", typ)
		}
	}
}
