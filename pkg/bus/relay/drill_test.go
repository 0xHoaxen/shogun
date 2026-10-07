package relay_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
)

const drillEvents = 5

var errConsumerDown = errors.New("consumer is down")

// flakyBus stands between the relay and a real consumer. While down it fails
// every delivery without reaching the consumer (a killed process); lostAcks
// deliveries reach the consumer, which commits, and then report an error (the
// consumer died after committing, before it answered).
type flakyBus struct {
	inner    bus.Bus
	down     atomic.Bool
	lostAcks atomic.Int32
}

func (b *flakyBus) Deliver(ctx context.Context, consumer string, env *eventsv1.Envelope) error {
	if b.down.Load() {
		return errConsumerDown
	}
	if err := b.inner.Deliver(ctx, consumer, env); err != nil {
		return err
	}
	if b.lostAcks.Add(-1) >= 0 {
		return errConsumerDown
	}
	return nil
}

func drillRelay(ctx context.Context, t *testing.T, flaky *flakyBus) int {
	t.Helper()
	_, producer := newPool(t)
	writeEvents(ctx, t, producer, "job.added", drillEvents)
	startRelay(ctx, t, relay.Config{
		Pool: producer, Bus: flaky, Interval: time.Hour, RetryPolicy: fastRetry{},
		FetchPollInterval: 100 * time.Millisecond,
		Routes:            routes(map[string][]string{"job.added": {"taiko"}}),
	})
	waitUndelivered(ctx, t, producer, 0)
	return drillEvents
}

func newFlakyBus(ctx context.Context, t *testing.T, consumer *consumerService) *flakyBus {
	t.Helper()
	client := dialClient(ctx, t, consumer)
	return &flakyBus{inner: bus.NewGRPCBus(map[string]eventsv1.EventSinkServiceClient{"taiko": client})}
}

func TestDrillConsumerKilledMidDeliveryLosesNothingAndAppliesOnce(t *testing.T) {
	// Arrange: the consumer is down when the events are relayed.
	ctx := context.Background()
	consumer := newConsumer(t, "taiko", 0)
	flaky := newFlakyBus(ctx, t, consumer)
	flaky.down.Store(true)

	// Act: relay while it is down, check nothing was applied, then bring it back.
	n := drillRelay(ctx, t, flaky)
	time.Sleep(300 * time.Millisecond)
	if _, applied := consumer.counts(); applied != 0 {
		t.Fatalf("applied while the consumer was down = %d, want 0", applied)
	}
	flaky.down.Store(false)
	waitApplied(t, consumer, n)

	// Assert: every event arrived, once.
	time.Sleep(300 * time.Millisecond)
	if invoked, applied := consumer.counts(); invoked != n || applied != n {
		t.Errorf("invoked=%d applied=%d, want %d each", invoked, applied, n)
	}
	if got := inboxRows(ctx, t, consumer); got != n {
		t.Errorf("inbox rows = %d, want %d", got, n)
	}
}

func TestDrillLostAcknowledgementIsDeduplicatedOnRedelivery(t *testing.T) {
	// Arrange: the first deliveries commit in the consumer but the answer is lost.
	ctx := context.Background()
	consumer := newConsumer(t, "taiko", 0)
	flaky := newFlakyBus(ctx, t, consumer)
	flaky.lostAcks.Store(drillEvents)

	// Act
	n := drillRelay(ctx, t, flaky)
	waitApplied(t, consumer, n)
	time.Sleep(500 * time.Millisecond)

	// Assert: redelivery reached the inbox but the handler ran once per event.
	if invoked, applied := consumer.counts(); invoked != n || applied != n {
		t.Errorf("invoked=%d applied=%d, want %d each", invoked, applied, n)
	}
	if got := inboxRows(ctx, t, consumer); got != n {
		t.Errorf("inbox rows = %d, want %d", got, n)
	}
}
