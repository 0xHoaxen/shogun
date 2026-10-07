package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

const waitFor = 5 * time.Second

func asOwner(owner uuid.UUID) context.Context {
	return authz.WithIdentity(context.Background(), authz.Identity{OwnerID: owner.String()})
}

func notification(owner uuid.UUID) db.Notification {
	return db.Notification{ID: store.NewID(), OwnerID: owner, Type: "draft_ready", Title: "Ready", CreatedAt: now}
}

// subscribed starts Subscribe for owner and returns once it is registered. The
// result of Subscribe arrives on the returned channel.
func subscribed(t *testing.T, svc *app.Service, owner uuid.UUID, send func(db.Notification) error) <-chan error {
	t.Helper()
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- svc.Subscribe(asOwner(owner), "", func() error { close(ready); return nil }, send)
	}()
	select {
	case <-ready:
	case <-time.After(waitFor):
		t.Fatal("subscribe never registered")
	}
	return done
}

func result(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(waitFor):
		t.Fatal("subscribe did not return")
		return nil
	}
}

func TestSubscribeClosesAStreamThatFallsBehind(t *testing.T) {
	svc, _ := newService(t)
	owner := uuid.New()
	gate := make(chan struct{})
	done := subscribed(t, svc, owner, func(db.Notification) error { <-gate; return nil })

	for range 100 { // far more than a stream may have waiting
		svc.Publish(notification(owner))
	}
	close(gate)

	if err := result(t, done); !errors.Is(err, app.ErrStreamReset) {
		t.Fatalf("got %v, want ErrStreamReset so the client reconnects and replays", err)
	}
}

func TestResetClosesEveryStream(t *testing.T) {
	svc, _ := newService(t)
	done := subscribed(t, svc, uuid.New(), func(db.Notification) error { return nil })

	svc.Reset()

	if err := result(t, done); !errors.Is(err, app.ErrStreamReset) {
		t.Fatalf("got %v, want ErrStreamReset", err)
	}
}

func TestSubscribeSendsOnlyTheOwnersNotificationsAndStopsWhenCancelled(t *testing.T) {
	svc, _ := newService(t)
	owner := uuid.New()
	got := make(chan db.Notification, 4)
	ctx, cancel := context.WithCancel(asOwner(owner))
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- svc.Subscribe(ctx, "", func() error { close(ready); return nil }, func(n db.Notification) error { got <- n; return nil })
	}()
	<-ready

	svc.Publish(notification(uuid.New()))
	mine := notification(owner)
	svc.Publish(mine)
	first := <-got
	cancel()

	if first.ID != mine.ID {
		t.Fatalf("got %s, want my notification %s", first.ID, mine.ID)
	}
	if err := result(t, done); err != nil {
		t.Fatalf("cancelling a stream is not an error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("another owner's notification was sent")
	}
}

func TestSubscribeStopsWhenSendFails(t *testing.T) {
	svc, _ := newService(t)
	owner := uuid.New()
	boom := errors.New("client went away")
	done := subscribed(t, svc, owner, func(db.Notification) error { return boom })

	svc.Publish(notification(owner))

	if err := result(t, done); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the send error", err)
	}
}

func TestSubscribeRefusesACallWithoutAnOwner(t *testing.T) {
	svc, _ := newService(t)

	err := svc.Subscribe(context.Background(), "", func() error { return nil }, func(db.Notification) error { return nil })

	if !errors.Is(err, app.ErrNoOwner) {
		t.Fatalf("got %v, want ErrNoOwner", err)
	}
}

func TestSubscribeRefusesABadAfterID(t *testing.T) {
	svc, _ := newService(t)

	err := svc.Subscribe(asOwner(uuid.New()), "nope", func() error { return nil }, func(db.Notification) error { return nil })

	var invalid *app.InvalidArgumentError
	if !errors.As(err, &invalid) || invalid.Reason != "INVALID_ID" {
		t.Fatalf("got %v, want INVALID_ID", err)
	}
}
