// Command kagami runs the kagami service.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/logger"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/server"
	"github.com/0xHoaxen/shogun/pkg/telemetry"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
	kagamievents "github.com/0xHoaxen/shogun/services/kagami/internal/events"
	"github.com/0xHoaxen/shogun/services/kagami/internal/jobs"
	kagamigrpc "github.com/0xHoaxen/shogun/services/kagami/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/kagami/migrations"
)

const (
	serviceName        = "kagami"
	migrateOnStartEnv  = "MIGRATE_ON_START"
	migrateOnStartDflt = "true"
	migrateCommand     = "migrate"
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
	if len(os.Args) > 1 {
		return runCommand(ctx, os.Args[1], os.LookupEnv)
	}
	return run(ctx, os.LookupEnv, relayOverrides{})
}

// runCommand runs a one-shot subcommand instead of serving.
func runCommand(ctx context.Context, name string, lookup config.LookupFunc) error {
	switch name {
	case migrateCommand:
		return runMigrate(ctx, lookup)
	default:
		return fmt.Errorf("unknown command %q", name)
	}
}

// runMigrate applies migrations and exits; the Helm pre-upgrade Job runs it.
func runMigrate(ctx context.Context, lookup config.LookupFunc) error {
	cfg, err := config.LoadBase(serviceName, lookup)
	if err != nil {
		return err
	}
	pool, err := postgres.Connect(ctx, cfg.DatabaseURL, serviceName)
	if err != nil {
		return err
	}
	defer pool.Close()
	return migrate(ctx, pool)
}

// run wires the service and blocks until ctx is cancelled or serving fails.
func run(ctx context.Context, lookup config.LookupFunc, overrides relayOverrides, opts ...server.Option) error {
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
		if err := migrate(ctx, pool); err != nil {
			return err
		}
	}

	service := app.NewService(pool, nil)
	sink, err := bus.NewSinkServer(pool, kagamievents.Handlers(service, log), log)
	if err != nil {
		return err
	}

	scheduled, err := jobs.NewSetup(service, time.Now, log)
	if err != nil {
		return err
	}
	stopRelay, err := startRelay(ctx, pool, log, lookup, authority, cfg.ShutdownTimeout, scheduled, overrides)
	if err != nil {
		return err
	}
	defer stopRelay()

	serverOpts := append([]server.Option{
		server.WithAuth(authority.UnaryServerInterceptor(), authority.StreamServerInterceptor()),
		server.WithReadinessCheck(pool.Ping),
	}, opts...)
	kagami := kagamigrpc.New(service)
	register := func(s *grpc.Server) {
		eventsv1.RegisterEventSinkServiceServer(s, sink)
		kagamiv1.RegisterKagamiServiceServer(s, kagami)
	}
	return server.Run(ctx, cfg, log, register, serverOpts...)
}

// migrate applies the service's own migrations, then the relay's River tables.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}
	return relay.Migrate(ctx, pool)
}

func flushTelemetry(log *slog.Logger, timeout time.Duration, shutdown func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		log.Error("telemetry shutdown", slog.Any("error", err))
	}
}
