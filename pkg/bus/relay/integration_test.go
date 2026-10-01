package relay_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
)

const bufSize = 1 << 20

// consumerService is an in-process service: its own database and schema, an
// inbox, a handler for job.added and a gRPC EventSinkService on bufconn.
type consumerService struct {
	pool  *pgxpool.Pool
	lis   *bufconn.Listener
	failN int // handler errors for the first failN invocations

	mu      sync.Mutex
	invoked int // times the handler body started
	applied int // times the handler's effects committed
}

func newConsumer(t *testing.T, schema string, failN int) *consumerService {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), schema)
	if err != nil {
		t.Fatalf("connect %s: %v", schema, err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, postgres.OutboxInboxSQL); err != nil {
		t.Fatalf("migrate %s: %v", schema, err)
	}

	c := &consumerService{pool: pool, lis: bufconn.Listen(bufSize), failN: failN}
	sink, err := bus.NewSinkServer(pool, map[string]bus.Handler{"job.added": c.handle}, nil)
	if err != nil {
		t.Fatalf("sink %s: %v", schema, err)
	}
	srv := grpc.NewServer()
	eventsv1.RegisterEventSinkServiceServer(srv, sink)
	go func() { _ = srv.Serve(c.lis) }()
	t.Cleanup(srv.Stop)
	return c
}

func (c *consumerService) handle(context.Context, pgx.Tx, *eventsv1.Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invoked++
	if c.invoked <= c.failN {
		return errors.New("transient handler failure")
	}
	c.applied++
	return nil
}

func (c *consumerService) counts() (invoked, applied int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.invoked, c.applied
}

func (c *consumerService) dialOption() grpc.DialOption {
	return grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return c.lis.Dial() })
}

func waitApplied(t *testing.T, c *consumerService, want int) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if _, applied := c.counts(); applied >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	invoked, applied := c.counts()
	t.Fatalf("timed out: applied=%d invoked=%d, want applied=%d", applied, invoked, want)
}

func dialClient(ctx context.Context, t *testing.T, c *consumerService) eventsv1.EventSinkServiceClient {
	t.Helper()
	conn, err := grpcclient.Dial(ctx, "passthrough:///consumer", grpcclient.WithDialOptions(c.dialOption()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return eventsv1.NewEventSinkServiceClient(conn)
}

func inboxRows(ctx context.Context, t *testing.T, c *consumerService) int {
	t.Helper()
	var n int
	if err := c.pool.QueryRow(ctx, `SELECT count(*) FROM inbox`).Scan(&n); err != nil {
		t.Fatalf("count inbox: %v", err)
	}
	return n
}

func TestEventFlowsFromOutboxToTwoConsumers(t *testing.T) {
	// Arrange: producer "kagami"; consumers "taiko" (handler fails twice) and "sensei".
	ctx, producer := newPool(t)
	taiko := newConsumer(t, "taiko", 2)
	sensei := newConsumer(t, "sensei", 0)
	taikoClient := dialClient(ctx, t, taiko)
	grpcBus := bus.NewGRPCBus(map[string]eventsv1.EventSinkServiceClient{
		"taiko":  taikoClient,
		"sensei": dialClient(ctx, t, sensei),
	})

	// Act: write one event, start the relay.
	ids := writeEvents(ctx, t, producer, "job.added", 1)
	startRelay(ctx, t, relay.Config{
		Pool: producer, Bus: grpcBus, Interval: time.Hour, RetryPolicy: fastRetry{},
		FetchPollInterval: 100 * time.Millisecond,
		Routes:            routes(map[string][]string{"job.added": {"taiko", "sensei"}}),
	})
	waitApplied(t, taiko, 1)
	waitApplied(t, sensei, 1)

	// Assert: each consumer applied the event once; taiko needed three tries.
	if invoked, applied := taiko.counts(); invoked != 3 || applied != 1 {
		t.Errorf("taiko invoked=%d applied=%d, want 3 and 1", invoked, applied)
	}
	if invoked, applied := sensei.counts(); invoked != 1 || applied != 1 {
		t.Errorf("sensei invoked=%d applied=%d, want 1 and 1", invoked, applied)
	}
	waitUndelivered(ctx, t, producer, 0)

	// A duplicate delivery of the same event is acknowledged but ignored.
	var raw []byte
	if err := producer.QueryRow(ctx, `SELECT payload FROM outbox WHERE id = $1`, ids[0]).Scan(&raw); err != nil {
		t.Fatalf("load payload: %v", err)
	}
	env := &eventsv1.Envelope{}
	if err := proto.Unmarshal(raw, env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, err := taikoClient.Deliver(ctx, &eventsv1.DeliverRequest{Envelope: env}); err != nil {
		t.Fatalf("duplicate deliver: %v", err)
	}
	if invoked, applied := taiko.counts(); invoked != 3 || applied != 1 {
		t.Errorf("after duplicate taiko invoked=%d applied=%d, want 3 and 1", invoked, applied)
	}
	if got := inboxRows(ctx, t, taiko); got != 1 {
		t.Errorf("taiko inbox rows = %d, want 1", got)
	}
	if got := inboxRows(ctx, t, sensei); got != 1 {
		t.Errorf("sensei inbox rows = %d, want 1", got)
	}
}
