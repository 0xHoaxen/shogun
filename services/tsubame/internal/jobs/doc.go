// Package jobs holds the tsubame River workers: the scheduled mail sync and,
// later, message classification.
//
// The workers share the outbox relay's River client (see relay.Config), so they
// are handed over as a Setup rather than started here.
package jobs
