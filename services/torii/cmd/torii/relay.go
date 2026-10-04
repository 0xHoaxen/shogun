package main

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
)

// relayOwnerPrefix builds the system identity relay deliveries run under.
// Events carry no owner, and consumers that enforce authz still need one.
const relayOwnerPrefix = "system:"

// runOverrides replaces what run would otherwise build from the environment:
// the relay's bus and event routes, and the public API listener. The zero
// value means no override.
type runOverrides struct {
	bus            bus.Bus
	routes         func(eventType string) []string
	publicListener net.Listener
}

// startRelay starts the outbox relay and returns a function that stops it and
// releases the connections it opened, waiting at most timeout.
func startRelay(
	ctx context.Context,
	pool *pgxpool.Pool,
	log *slog.Logger,
	lookup config.LookupFunc,
	signer grpcclient.Signer,
	timeout time.Duration,
	overrides runOverrides,
) (func(), error) {
	sink := overrides.bus
	closeBus := func() error { return nil }
	if sink == nil {
		dialed, err := bus.DialGRPCBus(ctx,
			bus.Targets(lookup, bus.AllConsumers()),
			[]grpcclient.Option{grpcclient.WithSigner(signer)},
			bus.WithIdentity(authz.Identity{OwnerID: relayOwnerPrefix + serviceName}),
		)
		if err != nil {
			return nil, err
		}
		sink, closeBus = dialed, dialed.Close
	}

	r, err := relay.New(relay.Config{Pool: pool, Bus: sink, Routes: overrides.routes, Logger: log})
	if err != nil {
		_ = closeBus()
		return nil, err
	}
	// Cancelling ctx must not abort in-flight deliveries; the returned stop
	// function drains them within the shutdown timeout instead.
	if err := r.Start(context.WithoutCancel(ctx)); err != nil {
		_ = closeBus()
		return nil, err
	}
	return func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := r.Stop(stopCtx); err != nil {
			log.Error("relay stop", slog.Any("error", err))
		}
		if err := closeBus(); err != nil {
			log.Error("relay bus close", slog.Any("error", err))
		}
	}, nil
}
