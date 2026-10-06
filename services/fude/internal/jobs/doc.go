// Package jobs holds the fude River workers: the one that writes AI versions
// of drafts and the one that embeds voice samples.
//
// The worker shares the outbox relay's River client (see relay.Config), so it
// is handed over as a Setup rather than started here.
package jobs
