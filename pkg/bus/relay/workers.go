package relay

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/postgres"
)

// Ordered by created_at then id so the partial undelivered index is used and
// UUIDv7 ids break ties in production order.
const selectUndeliveredSQL = `SELECT id, type FROM outbox
WHERE delivered_at IS NULL
ORDER BY created_at, id
LIMIT $1
FOR UPDATE SKIP LOCKED`

const markDeliveredSQL = `UPDATE outbox SET delivered_at = now() WHERE id = ANY($1)`

const loadPayloadSQL = `SELECT payload FROM outbox WHERE id = $1`

type pendingEvent struct {
	id        uuid.UUID
	eventType string
}

// relayWorker runs batches until the outbox has no undelivered rows left.
type relayWorker struct {
	river.WorkerDefaults[relayArgs]
	relay *Relay
}

func (w *relayWorker) Work(ctx context.Context, _ *river.Job[relayArgs]) error {
	for {
		n, err := w.relay.relayBatch(ctx)
		if err != nil {
			return err
		}
		if n < BatchSize {
			return nil
		}
	}
}

// relayBatch relays up to BatchSize rows in one transaction and returns how
// many rows it handled. Jobs and delivered_at commit together, so an event is
// never marked delivered without its jobs, nor enqueued twice.
func (r *Relay) relayBatch(ctx context.Context) (int, error) {
	client := river.ClientFromContext[pgxTx](ctx)
	handled := 0
	err := postgres.InTx(ctx, r.cfg.Pool, func(tx pgx.Tx) error {
		events, err := selectUndelivered(ctx, tx)
		if err != nil {
			return err
		}
		handled = len(events)
		if handled == 0 {
			return nil
		}
		jobs := r.deliverJobs(events)
		if len(jobs) > 0 {
			if _, err := client.InsertManyTx(ctx, tx, jobs); err != nil {
				return fmt.Errorf("relay: enqueue deliveries: %w", err)
			}
		}
		ids := make([]uuid.UUID, len(events))
		for i, e := range events {
			ids[i] = e.id
		}
		if _, err := tx.Exec(ctx, markDeliveredSQL, ids); err != nil {
			return fmt.Errorf("relay: mark delivered: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return handled, nil
}

func selectUndelivered(ctx context.Context, tx pgx.Tx) ([]pendingEvent, error) {
	rows, err := tx.Query(ctx, selectUndeliveredSQL, BatchSize)
	if err != nil {
		return nil, fmt.Errorf("relay: select undelivered: %w", err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pendingEvent, error) {
		var e pendingEvent
		err := row.Scan(&e.id, &e.eventType)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("relay: read undelivered: %w", err)
	}
	return events, nil
}

func (r *Relay) deliverJobs(events []pendingEvent) []river.InsertManyParams {
	var jobs []river.InsertManyParams
	for _, e := range events {
		for _, consumer := range r.cfg.Routes(e.eventType) {
			jobs = append(jobs, river.InsertManyParams{
				Args: DeliverEventArgs{EventID: e.id.String(), Consumer: consumer},
				InsertOpts: &river.InsertOpts{
					Queue:       queueDeliver,
					MaxAttempts: r.cfg.MaxAttempts,
				},
			})
		}
	}
	return jobs
}

// deliverWorker loads one event from the outbox and hands it to the bus.
type deliverWorker struct {
	river.WorkerDefaults[DeliverEventArgs]
	relay *Relay
}

func (w *deliverWorker) Work(ctx context.Context, job *river.Job[DeliverEventArgs]) error {
	id, err := uuid.Parse(job.Args.EventID)
	if err != nil {
		return river.JobCancel(fmt.Errorf("relay: invalid event id: %w", err))
	}
	env, err := w.relay.loadEnvelope(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return river.JobCancel(fmt.Errorf("relay: event %s not in outbox", id))
	}
	if err != nil {
		return err
	}
	if err := w.relay.cfg.Bus.Deliver(ctx, job.Args.Consumer, env); err != nil {
		w.relay.log.Warn("relay: delivery failed",
			"event_id", id.String(), "event_type", env.GetType(),
			"consumer", job.Args.Consumer, "attempt", job.Attempt, "error", err)
		return fmt.Errorf("relay: deliver to %s: %w", job.Args.Consumer, err)
	}
	return nil
}

func (r *Relay) loadEnvelope(ctx context.Context, id uuid.UUID) (*eventsv1.Envelope, error) {
	var raw []byte
	if err := r.cfg.Pool.QueryRow(ctx, loadPayloadSQL, id).Scan(&raw); err != nil {
		return nil, fmt.Errorf("relay: load event: %w", err)
	}
	env := &eventsv1.Envelope{}
	if err := proto.Unmarshal(raw, env); err != nil {
		return nil, river.JobCancel(fmt.Errorf("relay: unmarshal event %s: %w", id, err))
	}
	return env, nil
}
