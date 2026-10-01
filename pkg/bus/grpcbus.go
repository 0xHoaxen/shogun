package bus

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"google.golang.org/grpc"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
)

// GRPCBus is a Bus that delivers to consumer services through their
// EventSinkService gRPC endpoint.
type GRPCBus struct {
	clients  map[string]eventsv1.EventSinkServiceClient
	conns    []*grpc.ClientConn
	identity *authz.Identity
}

var _ Bus = (*GRPCBus)(nil)

// GRPCOption customises a GRPCBus. Options are pure: they return a modified copy.
type GRPCOption func(GRPCBus) GRPCBus

// WithIdentity stamps every delivery with id, for deployments where consumers
// enforce authz on all methods. Relay jobs have no caller identity of their
// own, so this is the system identity events are delivered under.
func WithIdentity(id authz.Identity) GRPCOption {
	return func(b GRPCBus) GRPCBus {
		b.identity = &id
		return b
	}
}

// NewGRPCBus builds a bus over ready-made clients keyed by consumer name.
// The map is copied. Closing is the caller's concern for these clients.
func NewGRPCBus(clients map[string]eventsv1.EventSinkServiceClient, opts ...GRPCOption) *GRPCBus {
	copied := make(map[string]eventsv1.EventSinkServiceClient, len(clients))
	for name, c := range clients {
		copied[name] = c
	}
	b := GRPCBus{clients: copied}
	for _, opt := range opts {
		b = opt(b)
	}
	return &b
}

// DialGRPCBus dials every consumer in targets (consumer name to address)
// with grpcclient.Dial and returns a bus that owns the connections; call
// Close to release them.
func DialGRPCBus(ctx context.Context, targets map[string]string, dialOpts []grpcclient.Option, opts ...GRPCOption) (*GRPCBus, error) {
	clients := make(map[string]eventsv1.EventSinkServiceClient, len(targets))
	var conns []*grpc.ClientConn
	closeAll := func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}
	for name, target := range targets {
		conn, err := grpcclient.Dial(ctx, target, dialOpts...)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("bus: dial consumer %s: %w", name, err)
		}
		conns = append(conns, conn)
		clients[name] = eventsv1.NewEventSinkServiceClient(conn)
	}
	b := NewGRPCBus(clients, opts...)
	b.conns = conns
	return b, nil
}

// Deliver sends env to consumer. An unknown consumer is an error rather than
// a silent drop, so a missing route configuration is noticed.
func (b *GRPCBus) Deliver(ctx context.Context, consumer string, env *eventsv1.Envelope) error {
	client, ok := b.clients[consumer]
	if !ok {
		return fmt.Errorf("bus: no endpoint configured for consumer %q (known: %v)", consumer, b.consumers())
	}
	if b.identity != nil {
		ctx = authz.WithIdentity(ctx, *b.identity)
	}
	if _, err := client.Deliver(ctx, &eventsv1.DeliverRequest{Envelope: env}); err != nil {
		return fmt.Errorf("bus: deliver to %s: %w", consumer, err)
	}
	return nil
}

// Close closes connections opened by DialGRPCBus.
func (b *GRPCBus) Close() error {
	var errs []error
	for _, c := range b.conns {
		errs = append(errs, c.Close())
	}
	return errors.Join(errs...)
}

func (b *GRPCBus) consumers() []string {
	names := make([]string, 0, len(b.clients))
	for name := range b.clients {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
