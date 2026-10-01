package relay_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/rivertype"
	"google.golang.org/protobuf/types/known/wrapperspb"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

const waitFor = 20 * time.Second

// fakeBus records deliveries and can fail the first N attempts per consumer.
type fakeBus struct {
	mu        sync.Mutex
	failFirst map[string]int
	attempts  map[string]int
	delivered []string // "consumer/eventID"
}

func newFakeBus(failFirst map[string]int) *fakeBus {
	return &fakeBus{failFirst: failFirst, attempts: map[string]int{}}
}

func (b *fakeBus) Deliver(_ context.Context, consumer string, env *eventsv1.Envelope) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempts[consumer]++
	if b.attempts[consumer] <= b.failFirst[consumer] {
		return errors.New("consumer unavailable")
	}
	b.delivered = append(b.delivered, consumer+"/"+env.GetId())
	return nil
}

func (b *fakeBus) snapshot() ([]string, map[string]int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	attempts := map[string]int{}
	for k, v := range b.attempts {
		attempts[k] = v
	}
	out := append([]string(nil), b.delivered...)
	sort.Strings(out)
	return out, attempts
}

func (b *fakeBus) waitDelivered(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if got, _ := b.snapshot(); len(got) >= n {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, attempts := b.snapshot()
	t.Fatalf("timed out waiting for %d deliveries; got %d, attempts %v", n, len(got), attempts)
	return nil
}

type fastRetry struct{}

func (fastRetry) NextRetry(*rivertype.JobRow) time.Time { return time.Now().Add(10 * time.Millisecond) }

func newPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "kagami")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, postgres.OutboxInboxSQL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := relay.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate river: %v", err)
	}
	return ctx, pool
}

func startRelay(ctx context.Context, t *testing.T, cfg relay.Config) {
	t.Helper()
	r, err := relay.New(cfg)
	if err != nil {
		t.Fatalf("new relay: %v", err)
	}
	if err := r.Start(ctx); err != nil {
		t.Fatalf("start relay: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.Stop(stopCtx); err != nil {
			t.Errorf("stop relay: %v", err)
		}
	})
}

func writeEvents(ctx context.Context, t *testing.T, pool *pgxpool.Pool, typ string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		for i := range n {
			id, err := outbox.Write(ctx, tx, "kagami", typ, fmt.Sprintf("subject-%d", i), wrapperspb.String("x"))
			if err != nil {
				return err
			}
			ids = append(ids, id.String())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("write events: %v", err)
	}
	return ids
}

func routes(table map[string][]string) func(string) []string {
	return func(typ string) []string { return table[typ] }
}

func undelivered(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE delivered_at IS NULL`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func waitUndelivered(ctx context.Context, t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if undelivered(ctx, t, pool) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("undelivered rows = %d, want %d", undelivered(ctx, t, pool), want)
}

func TestRelayDeliversOncePerConsumerAndMarksDelivered(t *testing.T) {
	ctx, pool := newPool(t)
	ids := writeEvents(ctx, t, pool, "job.added", 1)
	fb := newFakeBus(nil)

	startRelay(ctx, t, relay.Config{
		Pool: pool, Bus: fb, Interval: time.Hour,
		Routes: routes(map[string][]string{"job.added": {"taiko", "sensei"}}),
	})

	got := fb.waitDelivered(t, 2)
	want := []string{"sensei/" + ids[0], "taiko/" + ids[0]}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("delivered = %v, want %v", got, want)
	}
	waitUndelivered(ctx, t, pool, 0)
}

func TestRelayMarksUnroutedEventsDeliveredWithoutDelivering(t *testing.T) {
	ctx, pool := newPool(t)
	writeEvents(ctx, t, pool, "nobody.cares", 3)
	fb := newFakeBus(nil)

	startRelay(ctx, t, relay.Config{Pool: pool, Bus: fb, Interval: time.Hour, Routes: routes(nil)})

	waitUndelivered(ctx, t, pool, 0)
	if got, _ := fb.snapshot(); len(got) != 0 {
		t.Fatalf("delivered = %v, want none", got)
	}
}

func TestRelayRetriesFailedDelivery(t *testing.T) {
	ctx, pool := newPool(t)
	ids := writeEvents(ctx, t, pool, "job.added", 1)
	fb := newFakeBus(map[string]int{"taiko": 2})

	startRelay(ctx, t, relay.Config{
		Pool: pool, Bus: fb, Interval: time.Hour, RetryPolicy: fastRetry{},
		FetchPollInterval: 100 * time.Millisecond,
		Routes:            routes(map[string][]string{"job.added": {"taiko", "sensei"}}),
	})

	got := fb.waitDelivered(t, 2)
	if len(got) != 2 {
		t.Fatalf("delivered = %v, want one per consumer", got)
	}
	_, attempts := fb.snapshot()
	if attempts["taiko"] != 3 || attempts["sensei"] != 1 {
		t.Fatalf("attempts = %v, want taiko 3 and sensei 1", attempts)
	}
	if got[1] != "taiko/"+ids[0] {
		t.Fatalf("delivered = %v", got)
	}
}

func TestRelayRelaysMoreThanOneBatch(t *testing.T) {
	ctx, pool := newPool(t)
	const total = relay.BatchSize + 50
	writeEvents(ctx, t, pool, "job.added", total)
	fb := newFakeBus(nil)

	startRelay(ctx, t, relay.Config{
		Pool: pool, Bus: fb, Interval: time.Hour,
		Routes: routes(map[string][]string{"job.added": {"taiko"}}),
	})

	if got := fb.waitDelivered(t, total); len(got) != total {
		t.Fatalf("delivered %d, want %d", len(got), total)
	}
	waitUndelivered(ctx, t, pool, 0)
}

func TestRelayIsWokenByNotifyWithoutWaitingForInterval(t *testing.T) {
	ctx, pool := newPool(t)
	fb := newFakeBus(nil)
	startRelay(ctx, t, relay.Config{
		Pool: pool, Bus: fb, Interval: time.Hour,
		Routes: routes(map[string][]string{"job.added": {"taiko"}}),
	})
	// Let the initial run-on-start pass and the listener subscribe.
	time.Sleep(time.Second)

	writeEvents(ctx, t, pool, "job.added", 1)
	fb.waitDelivered(t, 1)

	// A second wake-up proves the listener survives the debounce wait.
	writeEvents(ctx, t, pool, "job.added", 1)
	fb.waitDelivered(t, 2)
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	if _, err := relay.New(relay.Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
}
