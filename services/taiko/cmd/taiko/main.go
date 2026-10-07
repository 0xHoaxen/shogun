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

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/logger"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/server"
	"github.com/0xHoaxen/shogun/pkg/telemetry"
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/events"
	"github.com/0xHoaxen/shogun/services/taiko/internal/jobs"
	"github.com/0xHoaxen/shogun/services/taiko/internal/live"
	"github.com/0xHoaxen/shogun/services/taiko/internal/transport/downstream"
	taikogrpc "github.com/0xHoaxen/shogun/services/taiko/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/taiko/migrations"
)

const (
	serviceName        = "taiko"
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

	svc := app.NewService(pool, nil)
	sink, err := bus.NewSinkServer(pool, events.Handlers(svc, log), log)
	if err != nil {
		return err
	}

	stopLive, err := live.New(pool, svc, log).Start(ctx)
	if err != nil {
		return err
	}
	defer stopLive()

	scheduled, closeDownstream, err := digestSetup(ctx, svc, lookup, authority, log)
	if err != nil {
		return err
	}
	defer closeDownstream()

	stopRelay, err := startRelay(ctx, pool, log, lookup, authority, cfg.ShutdownTimeout, scheduled, overrides)
	if err != nil {
		return err
	}
	defer stopRelay()

	serverOpts := append([]server.Option{
		server.WithAuth(authority.UnaryServerInterceptor(), authority.StreamServerInterceptor()),
		server.WithReadinessCheck(pool.Ping),
	}, opts...)
	register := func(s *grpc.Server) {
		eventsv1.RegisterEventSinkServiceServer(s, sink)
		taikov1.RegisterTaikoServiceServer(s, taikogrpc.New(svc))
	}
	return server.Run(ctx, cfg, log, register, serverOpts...)
}

// digestSetup dials the services the daily digest reads from and returns its
// scheduled job, with a function that closes the connections.
func digestSetup(
	ctx context.Context, svc *app.Service, lookup config.LookupFunc, signer grpcclient.Signer, log *slog.Logger,
) (jobs.Setup, func(), error) {
	loc, err := jobs.Location()
	if err != nil {
		return jobs.Setup{}, nil, err
	}
	var conns []*grpc.ClientConn
	closeAll := func() {
		for _, conn := range conns {
			if err := conn.Close(); err != nil {
				log.Warn("close connection", slog.Any("error", err))
			}
		}
	}
	dial := func(env string) (*grpc.ClientConn, error) {
		addr, err := config.Required(lookup, env)
		if err != nil {
			return nil, err
		}
		conn, err := grpcclient.Dial(ctx, addr, grpcclient.WithSigner(signer))
		if err != nil {
			return nil, fmt.Errorf("dial %s: %w", env, err)
		}
		conns = append(conns, conn)
		return conn, nil
	}
	kagami, err := dial("KAGAMI_ADDR")
	if err != nil {
		closeAll()
		return jobs.Setup{}, nil, err
	}
	fude, err := dial("FUDE_ADDR")
	if err != nil {
		closeAll()
		return jobs.Setup{}, nil, err
	}
	soroban, err := dial("SOROBAN_ADDR")
	if err != nil {
		closeAll()
		return jobs.Setup{}, nil, err
	}
	sources := downstream.New(
		kagamiv1.NewKagamiServiceClient(kagami), fudev1.NewFudeServiceClient(fude), sorobanv1.NewSorobanServiceClient(soroban))
	digester := app.NewDigester(svc, sources, loc, time.Now, log)
	return jobs.NewSetup(digester, loc, time.Now, log), closeAll, nil
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
