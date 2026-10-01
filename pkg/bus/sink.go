package bus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/inbox"
)

// Handler processes one event inside the transaction that records it in the
// inbox. Writing to the database through tx makes the handler's effects and
// the "seen" marker atomic; returning an error rolls both back so the
// producer's retry runs the handler again.
type Handler func(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error

// SinkServer implements eventsv1.EventSinkServiceServer for one service. It
// routes each delivered event to the Handler registered for its type and
// dedupes on the event id through the inbox.
type SinkServer struct {
	eventsv1.UnimplementedEventSinkServiceServer

	pool     *pgxpool.Pool
	handlers map[string]Handler
	log      *slog.Logger
}

// NewSinkServer builds a sink over pool (the service's own pool, whose schema
// holds the inbox table). handlers maps event type to Handler; the map is
// copied. A nil logger uses slog.Default().
func NewSinkServer(pool *pgxpool.Pool, handlers map[string]Handler, logger *slog.Logger) (*SinkServer, error) {
	if pool == nil {
		return nil, errors.New("bus: nil pool")
	}
	for typ, h := range handlers {
		if typ == "" || h == nil {
			return nil, fmt.Errorf("bus: invalid handler registration for type %q", typ)
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	copied := make(map[string]Handler, len(handlers))
	for typ, h := range handlers {
		copied[typ] = h
	}
	return &SinkServer{pool: pool, handlers: copied, log: logger.With("component", "event_sink")}, nil
}

// Deliver handles one event. Unknown event types are acknowledged and logged
// (type, id and source only, never the payload) so a producer is never stuck
// retrying an event this service does not consume.
func (s *SinkServer) Deliver(ctx context.Context, req *eventsv1.DeliverRequest) (*eventsv1.DeliverResponse, error) {
	env := req.GetEnvelope()
	if env == nil || env.GetId() == "" || env.GetType() == "" {
		return nil, status.Error(codes.InvalidArgument, "envelope with id and type is required")
	}
	handler, ok := s.handlers[env.GetType()]
	if !ok {
		s.log.Warn("event_sink: no handler for event type, acknowledging",
			"event_id", env.GetId(), "event_type", env.GetType(), "source", env.GetSource())
		return &eventsv1.DeliverResponse{}, nil
	}

	err := inbox.Handle(ctx, s.pool, env, func(ctx context.Context, tx pgx.Tx) error {
		return handler(ctx, tx, env)
	})
	if err != nil {
		s.log.Error("event_sink: handling failed",
			"event_id", env.GetId(), "event_type", env.GetType(), "error", err)
		return nil, status.Error(codes.Internal, "event handling failed")
	}
	return &eventsv1.DeliverResponse{}, nil
}
