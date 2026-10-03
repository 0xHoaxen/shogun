// Package relay moves events from a service's outbox table to their consumers.
//
// A River periodic job (woken early by LISTEN on the outbox channel) reads
// undelivered outbox rows, enqueues one deliver_event job per consumer route
// in the same transaction that marks the rows delivered, and a deliver_event
// worker hands each event to a bus.Bus. River retries failed deliveries with
// backoff; consumers dedupe on the event id through the inbox, so redelivery
// is safe. River's tables live in the service schema.
package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/postgres"
)

const (
	// BatchSize is the number of outbox rows relayed per transaction.
	BatchSize = 100

	defaultInterval       = 5 * time.Second
	defaultDeliverWorkers = 10
	defaultMaxAttempts    = 25
)

// Config configures a Relay. Pool and Bus are required.
type Config struct {
	// Pool must come from postgres.Connect; its schema holds the outbox and
	// River tables.
	Pool *pgxpool.Pool
	// Bus delivers events to consumers.
	Bus bus.Bus
	// Routes returns the consumers of an event type. Defaults to bus.Consumers.
	Routes func(eventType string) []string
	// Interval is how often the outbox is polled when no NOTIFY arrives.
	// Defaults to 5 seconds.
	Interval time.Duration
	// DeliverWorkers bounds concurrent deliveries. Defaults to 10.
	DeliverWorkers int
	// MaxAttempts bounds deliveries per event and consumer. Defaults to 25.
	MaxAttempts int
	// RetryPolicy overrides River's default exponential backoff.
	RetryPolicy river.ClientRetryPolicy
	// FetchPollInterval overrides how often River polls for runnable jobs.
	FetchPollInterval time.Duration
	// Workers registers a service's own River workers on the relay's client.
	// A service runs one River client, because periodic jobs run only on the
	// elected leader and two clients in one schema would compete for it.
	Workers func(*river.Workers)
	// Queues adds queues for the service's own jobs. The relay's queue names
	// are reserved.
	Queues map[string]river.QueueConfig
	// PeriodicJobs are the service's own periodic jobs.
	PeriodicJobs []*river.PeriodicJob
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

// Relay runs the outbox relay for one service.
type Relay struct {
	cfg    Config
	client *river.Client[pgxTx]
	log    *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// Migrate applies River's migrations to the pool's schema. Call it after
// postgres.Migrate has created the schema, before New.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	schema, err := schemaOf(pool)
	if err != nil {
		return err
	}
	return migrateRiver(ctx, pool, schema)
}

// New builds a Relay. Call Start to run it and Stop to shut it down.
func New(cfg Config) (*Relay, error) {
	if cfg.Pool == nil || cfg.Bus == nil {
		return nil, errors.New("relay: Pool and Bus are required")
	}
	schema, err := schemaOf(cfg.Pool)
	if err != nil {
		return nil, err
	}
	if cfg.Routes == nil {
		cfg.Routes = bus.Consumers
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.DeliverWorkers <= 0 {
		cfg.DeliverWorkers = defaultDeliverWorkers
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultMaxAttempts
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	r := &Relay{cfg: cfg, log: cfg.Logger.With("component", "relay")}

	workers := river.NewWorkers()
	river.AddWorker(workers, &relayWorker{relay: r})
	river.AddWorker(workers, &deliverWorker{relay: r})
	if cfg.Workers != nil {
		cfg.Workers(workers)
	}

	queues := map[string]river.QueueConfig{
		queueRelay:   {MaxWorkers: 1},
		queueDeliver: {MaxWorkers: cfg.DeliverWorkers},
	}
	for name, queue := range cfg.Queues {
		if _, reserved := queues[name]; reserved {
			return nil, fmt.Errorf("relay: queue %q is reserved for the relay", name)
		}
		queues[name] = queue
	}

	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(cfg.Interval),
			func() (river.JobArgs, *river.InsertOpts) { return relayArgs{}, relayInsertOpts() },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}
	periodic = append(periodic, cfg.PeriodicJobs...)

	client, err := river.NewClient(riverpgxv5.New(cfg.Pool), &river.Config{
		Schema:            schema,
		Logger:            r.log,
		RetryPolicy:       cfg.RetryPolicy,
		FetchPollInterval: cfg.FetchPollInterval,
		Queues:            queues,
		Workers:           workers,
		PeriodicJobs:      periodic,
	})
	if err != nil {
		return nil, fmt.Errorf("relay: create river client: %w", err)
	}
	r.client = client
	return r, nil
}

// Start launches River and the NOTIFY listener. It returns once both are
// running; they stop when ctx is cancelled or Stop is called.
func (r *Relay) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return errors.New("relay: already started")
	}
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("relay: start river: %w", err)
	}
	listenCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		r.listen(listenCtx)
	}()
	return nil
}

// Stop stops the listener and waits for River to finish running jobs, until
// ctx expires.
func (r *Relay) Stop(ctx context.Context) error {
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	<-done
	if err := r.client.Stop(ctx); err != nil {
		return fmt.Errorf("relay: stop river: %w", err)
	}
	return nil
}

// kick asks for an extra relay run soon. Duplicates are collapsed by the
// job's uniqueness options, so it is cheap to call often.
func (r *Relay) kick(ctx context.Context) {
	if _, err := r.client.Insert(ctx, relayArgs{}, relayInsertOpts()); err != nil && ctx.Err() == nil {
		r.log.Warn("relay: kick failed", "error", err)
	}
}

func schemaOf(pool *pgxpool.Pool) (string, error) {
	schema, err := postgres.SchemaOf(pool)
	if err != nil {
		return "", fmt.Errorf("relay: %w", err)
	}
	return schema, nil
}
