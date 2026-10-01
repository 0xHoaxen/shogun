// Command taiko runs the taiko service.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/logger"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/server"
	"github.com/0xHoaxen/shogun/pkg/telemetry"
	"github.com/0xHoaxen/shogun/services/taiko/migrations"
)

const (
	serviceName        = "taiko"
	migrateOnStartEnv  = "MIGRATE_ON_START"
	migrateOnStartDflt = "true"
)

func main() {
	if err := runMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", serviceName, err)
		os.Exit(1)
	}
}

func runMain() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return run(ctx, os.LookupEnv)
}

// run wires the service and blocks until ctx is cancelled or serving fails.
func run(ctx context.Context, lookup config.LookupFunc, opts ...server.Option) error {
	cfg, err := config.LoadBase(serviceName, lookup)
	if err != nil {
		return err
	}
	log := logger.New(os.Stdout, cfg.Service, cfg.LogLevel)

	key, err := authz.LoadKey(lookup)
	if err != nil {
		return err
	}
	authority, err := authz.New(key)
	if err != nil {
		return err
	}

	shutdownTelemetry, err := telemetry.Setup(ctx, cfg)
	if err != nil {
		return err
	}
	defer flushTelemetry(log, cfg.ShutdownTimeout, shutdownTelemetry)

	pool, err := postgres.Connect(ctx, cfg.DatabaseURL, serviceName)
	if err != nil {
		return err
	}
	defer pool.Close()

	if config.String(lookup, migrateOnStartEnv, migrateOnStartDflt) == "true" {
		if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
			return err
		}
	}

	sink, err := bus.NewSinkServer(pool, map[string]bus.Handler{}, log)
	if err != nil {
		return err
	}

	serverOpts := append([]server.Option{
		server.WithAuth(authority.UnaryServerInterceptor(), authority.StreamServerInterceptor()),
		server.WithReadinessCheck(pool.Ping),
	}, opts...)
	register := func(s *grpc.Server) {
		eventsv1.RegisterEventSinkServiceServer(s, sink)
	}
	return server.Run(ctx, cfg, log, register, serverOpts...)
}

func flushTelemetry(log *slog.Logger, timeout time.Duration, shutdown func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		log.Error("telemetry shutdown", slog.Any("error", err))
	}
}
