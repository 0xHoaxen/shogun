package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/jobs"
)

const (
	sorobanAddrEnv = "SOROBAN_ADDR"
)

// suggestions is what writing suggestions needs, with the connection to close.
type suggestions struct {
	option app.Option
	close  func()
}

// newSuggestions connects to soroban and builds the metered model client. The
// model is each user's own: soroban hands back their provider when it reserves a
// call, and a user with none gets no suggestions.
func newSuggestions(
	ctx context.Context, lookup config.LookupFunc, cfg config.Base, pool *pgxpool.Pool, signer grpcclient.Signer, log *slog.Logger,
) (*suggestions, error) {
	sorobanAddr, err := config.Required(lookup, sorobanAddrEnv)
	if err != nil {
		return nil, err
	}
	queue, err := jobs.NewRiverQueue(pool)
	if err != nil {
		return nil, err
	}
	conn, err := grpcclient.Dial(ctx, sorobanAddr, grpcclient.WithSigner(signer))
	if err != nil {
		return nil, err
	}
	completer, err := newCompleter(cfg, sorobanv1.NewSorobanServiceClient(conn), log)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &suggestions{
		option: app.WithSuggestions(queue, completer, log),
		close:  func() { closeConn(conn, log) },
	}, nil
}

func closeConn(conn *grpc.ClientConn, log *slog.Logger) {
	if err := conn.Close(); err != nil {
		log.Warn("close soroban connection", slog.Any("error", err))
	}
}

// newCompleter returns the metered model client. It holds no credentials: every
// call is made with the provider of the user it is for.
func newCompleter(cfg config.Base, meter llm.Meter, log *slog.Logger) (app.Completer, error) {
	llmCfg, err := llm.NewConfig(cfg.Service)
	if err != nil {
		return nil, err
	}
	return llm.New(llmCfg, llm.NewAnthropicAPI(llm.ForEnvironment(cfg.Environment)...), meter, llm.WithLogger(log))
}
