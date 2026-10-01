package relay

import (
	"github.com/riverqueue/river"
)

const (
	kindRelayOutbox = "relay_outbox"
	kindDeliver     = "deliver_event"

	queueRelay   = "relay"
	queueDeliver = "deliver"
)

// relayArgs is the argument-less job that moves one or more batches of
// undelivered outbox rows into deliver_event jobs.
type relayArgs struct{}

func (relayArgs) Kind() string { return kindRelayOutbox }

// DeliverEventArgs identifies one event to deliver to one consumer. The event
// itself stays in the outbox table; only its id travels through River.
type DeliverEventArgs struct {
	EventID  string `json:"event_id"`
	Consumer string `json:"consumer"`
}

// Kind implements river.JobArgs.
func (DeliverEventArgs) Kind() string { return kindDeliver }

// relayInsertOpts queues a relay run on its own queue. Uniqueness is avoided
// on purpose: River's unique states always include running, so a wake-up that
// arrives mid-run would be dropped although that run may already have read
// the outbox. NOTIFY bursts are coalesced by the listener instead.
func relayInsertOpts() *river.InsertOpts {
	return &river.InsertOpts{Queue: queueRelay}
}
