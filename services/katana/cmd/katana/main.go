// Command katana runs the katana service.
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
	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/logger"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/server"
	"github.com/0xHoaxen/shogun/pkg/telemetry"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/events"
	"github.com/0xHoaxen/shogun/services/katana/internal/github"
	"github.com/0xHoaxen/shogun/services/katana/internal/jobs"
	katanagrpc "github.com/0xHoaxen/shogun/services/katana/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/katana/migrations"
)

const (
	serviceName        = "katana"
	migrateOnStartEnv  = "MIGRATE_ON_START"
	migrateOnStartDflt = "true"
	migrateCommand     = "migrate"

	githubUserEnv    = "KATANA_GITHUB_USER"
	githubTokenEnv   = "KATANA_GITHUB_TOKEN"
	githubAPIBaseEnv = "KATANA_GITHUB_API_BASE"
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

	gh, err := newGitHub(lookup, cfg, log)
	if err != nil {
		return err
	}
	suggest, err := newSuggestions(ctx, lookup, cfg, pool, authority, log)
	if err != nil {
		return err
	}
	defer suggest.close()
	svc := app.NewService(pool, gh, nil, suggest.option)
	loc, err := jobs.Location()
	if err != nil {
		return err
	}
	scheduled := jobs.NewSetup(svc, svc, loc, time.Now, log)

	sink, err := bus.NewSinkServer(pool, events.Handlers(svc, log), log)
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
	register := func(s *grpc.Server) {
		eventsv1.RegisterEventSinkServiceServer(s, sink)
		katanav1.RegisterKatanaServiceServer(s, katanagrpc.New(svc))
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

// errNoGitHub means production has no GitHub user or token.
var errNoGitHub = fmt.Errorf("%s and %s are required in production", githubUserEnv, githubTokenEnv)

// newGitHub builds the GitHub client from the environment. Both the user and
// the token are required in production; elsewhere a missing one is logged, the
// service still starts, and syncing says GitHub is not configured. A nil
// client is returned for that case, which app.NewService accepts.
//
// TODO(owner): set KATANA_GITHUB_USER and KATANA_GITHUB_TOKEN (a token that can
// read your repositories) as secrets in the staging and production helm values.
func newGitHub(lookup config.LookupFunc, cfg config.Base, log *slog.Logger) (app.GitHub, error) {
	user := config.String(lookup, githubUserEnv, "")
	token := config.String(lookup, githubTokenEnv, "")
	if user == "" || token == "" {
		if cfg.IsProduction() {
			return nil, errNoGitHub
		}
		log.Warn("github sync is off: no github user or token configured")
		return nil, nil //nolint:nilnil // no client is how an unconfigured GitHub is passed on
	}
	return github.New(config.String(lookup, githubAPIBaseEnv, ""), user, token, nil), nil
}
