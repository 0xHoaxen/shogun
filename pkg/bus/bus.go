// Package bus delivers outbox events to consumer services. The relay reads
// the outbox and calls Bus.Deliver once per consumer route; the transport
// behind Bus (gRPC EventSink in production, a fake in tests) is replaceable.
package bus

import (
	"context"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
)

// Bus delivers one event to one consumer service.
//
// Implementations must be safe for concurrent use. Deliver may be called more
// than once for the same event (retries); consumers dedupe on env.Id through
// the inbox. A non-nil error asks the caller to retry later.
type Bus interface {
	Deliver(ctx context.Context, consumer string, env *eventsv1.Envelope) error
}
