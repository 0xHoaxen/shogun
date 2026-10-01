// Package outbox writes domain events to the service's outbox table in the
// same transaction as the state change they describe. A relay (P1.6) delivers
// the rows later.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
)

// Channel is the Postgres NOTIFY channel signalled for every written event.
const Channel = "outbox"

const insertSQL = `INSERT INTO outbox (id, type, source, subject, payload, traceparent, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`

// Write inserts an event into the outbox using tx and issues NOTIFY on
// Channel with the event id. Both take effect only when tx commits. The
// payload column stores the marshalled eventsv1.Envelope wrapping payload.
// It returns the new event id (UUIDv7). Payloads are never logged.
func Write(ctx context.Context, tx pgx.Tx, source, typ, subject string, payload proto.Message) (uuid.UUID, error) {
	if tx == nil {
		return uuid.Nil, errors.New("outbox: nil transaction")
	}
	if source == "" || typ == "" {
		return uuid.Nil, errors.New("outbox: source and type are required")
	}
	if payload == nil {
		return uuid.Nil, errors.New("outbox: nil payload")
	}
	packed, err := anypb.New(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: wrap payload: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: generate id: %w", err)
	}

	now := time.Now().UTC()
	// Traceparent stays empty until pkg/telemetry can supply it from ctx.
	env := &eventsv1.Envelope{
		Id:         id.String(),
		Type:       typ,
		Source:     source,
		Subject:    subject,
		OccurredAt: timestamppb.New(now),
		Payload:    packed,
	}
	raw, err := proto.Marshal(env)
	if err != nil {
		return uuid.Nil, fmt.Errorf("outbox: marshal envelope: %w", err)
	}

	if _, err := tx.Exec(ctx, insertSQL, id, typ, source, subject, raw, env.GetTraceparent(), now); err != nil {
		return uuid.Nil, fmt.Errorf("outbox: insert: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, Channel, id.String()); err != nil {
		return uuid.Nil, fmt.Errorf("outbox: notify: %w", err)
	}
	return id, nil
}
