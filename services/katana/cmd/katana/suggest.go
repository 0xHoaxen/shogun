package main

import (
	"context"
	"errors"
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
	apiKeyEnv      = "ANTHROPIC_API_KEY"
	baseURLEnv     = "ANTHROPIC_BASE_URL"
)

// errNoAPIKey means suggestions cannot be written: there is no model to ask.
var errNoAPIKey = errors.New(apiKeyEnv + " is not set")

// suggestions is what writing suggestions needs, with the connection to close.
type suggestions struct {
	option app.Option
	close  func()
}

// newSuggestions connects to soroban and builds the metered model client. The
// API key is required in production; elsewhere a missing key is logged and runs
// fail with a clear reason, so the local stack still starts.
func newSuggestions(
	ctx context.Context, lookup config.LookupFunc, cfg config.Base, pool *pgxpool.Pool, signer grpcclient.Signer, log *slog.Logger,
) (*suggestions, error) {
	sorobanAddr, err := config.Required(lookup, sorobanAddrEnv)
	if err != nil {
		return nil, err
	}
	apiKey := config.String(lookup, apiKeyEnv, "")
	if apiKey == "" && cfg.IsProduction() {
		return nil, errNoAPIKey
	}
	queue, err := jobs.NewRiverQueue(pool)
	if err != nil {
		return nil, err
	}
	conn, err := grpcclient.Dial(ctx, sorobanAddr, grpcclient.WithSigner(signer))
	if err != nil {
		return nil, err
	}
	completer, err := newCompleter(lookup, cfg, apiKey, sorobanv1.NewSorobanServiceClient(conn), log)
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

// newCompleter returns the metered Claude client, or one that fails every call
// when there is no API key.
func newCompleter(lookup config.LookupFunc, cfg config.Base, apiKey string, meter llm.Meter, log *slog.Logger) (app.Completer, error) {
	if apiKey == "" {
		log.Warn("suggestions are off: " + errNoAPIKey.Error())
		return noKeyCompleter{}, nil
	}
	llmCfg, err := llm.NewConfig(cfg.Service, lookup)
	if err != nil {
		return nil, err
	}
	return llm.New(llmCfg, llm.NewAnthropicAPI(apiKey, llm.AnthropicBaseURL(config.String(lookup, baseURLEnv, ""))), meter, llm.WithLogger(log))
}

type noKeyCompleter struct{}

func (noKeyCompleter) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	return llm.Response{}, errNoAPIKey
}
