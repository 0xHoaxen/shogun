package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/jobs"
	kagamisource "github.com/0xHoaxen/shogun/services/fude/internal/transport/kagami"
	tsubamemailer "github.com/0xHoaxen/shogun/services/fude/internal/transport/tsubame"
)

const (
	tsubameAddrEnv = "TSUBAME_ADDR"
	sorobanAddrEnv = "SOROBAN_ADDR"
	kagamiAddrEnv  = "KAGAMI_ADDR"
	apiKeyEnv      = "ANTHROPIC_API_KEY"
	baseURLEnv     = "ANTHROPIC_BASE_URL"
)

// errNoAPIKey is what every generation fails with while no API key is set.
var errNoAPIKey = errors.New(apiKeyEnv + " is not set")

// generation is what draft generation needs wired: the River worker for the
// relay's client, the queue the use cases insert through, and a function that
// releases the connections.
type generation struct {
	setup  jobs.Setup
	queue  *jobs.RiverQueue
	mailer app.Mailer
	close  func()
}

// newGeneration connects to soroban and kagami and builds the generator. The
// API key is required in production; elsewhere a missing key is logged and
// generation fails with a clear reason, so the local stack still starts.
func newGeneration(
	ctx context.Context,
	lookup config.LookupFunc,
	cfg config.Base,
	pool *pgxpool.Pool,
	signer grpcclient.Signer,
	log *slog.Logger,
) (*generation, error) {
	sorobanAddr, err := config.Required(lookup, sorobanAddrEnv)
	if err != nil {
		return nil, err
	}
	kagamiAddr, err := config.Required(lookup, kagamiAddrEnv)
	if err != nil {
		return nil, err
	}
	tsubameAddr, err := config.Required(lookup, tsubameAddrEnv)
	if err != nil {
		return nil, err
	}
	apiKey := config.String(lookup, apiKeyEnv, "")
	if apiKey == "" && cfg.IsProduction() {
		return nil, errNoAPIKey
	}
	// TODO(owner): choose the embedding provider. With none, voice samples are
	// stored unembedded and drafts use the newest ones.
	var embedder app.Embedder
	queue, err := jobs.NewRiverQueue(pool, embedder != nil)
	if err != nil {
		return nil, err
	}

	sorobanConn, err := grpcclient.Dial(ctx, sorobanAddr, grpcclient.WithSigner(signer))
	if err != nil {
		return nil, fmt.Errorf("dial soroban: %w", err)
	}
	kagamiConn, err := grpcclient.Dial(ctx, kagamiAddr, grpcclient.WithSigner(signer))
	if err != nil {
		_ = sorobanConn.Close()
		return nil, fmt.Errorf("dial kagami: %w", err)
	}
	tsubameConn, err := grpcclient.Dial(ctx, tsubameAddr, grpcclient.WithSigner(signer))
	if err != nil {
		_ = sorobanConn.Close()
		_ = kagamiConn.Close()
		return nil, fmt.Errorf("dial tsubame: %w", err)
	}
	closeConns := func() { _ = sorobanConn.Close(); _ = kagamiConn.Close(); _ = tsubameConn.Close() }

	completer, err := newCompleter(lookup, apiKey, sorobanv1.NewSorobanServiceClient(sorobanConn), log)
	if err != nil {
		closeConns()
		return nil, err
	}
	source := kagamisource.New(kagamiv1.NewKagamiServiceClient(kagamiConn))
	generator := app.NewGenerator(pool, completer, source, log, nil, app.WithEmbedder(embedder))
	var sampleEmbedder jobs.SampleEmbedder
	if embedder != nil {
		sampleEmbedder = app.NewVoiceEmbedder(pool, embedder)
	}
	return &generation{
		queue: queue, close: closeConns, setup: jobs.NewSetup(generator, sampleEmbedder, log),
		mailer: tsubamemailer.New(tsubamev1.NewTsubameServiceClient(tsubameConn)),
	}, nil
}

// newCompleter returns the metered Claude client, or one that fails every call
// when there is no API key.
func newCompleter(lookup config.LookupFunc, apiKey string, meter llm.Meter, log *slog.Logger) (app.Completer, error) {
	if apiKey == "" {
		log.Warn("generation is off: " + errNoAPIKey.Error())
		return noKeyCompleter{}, nil
	}
	cfg, err := llm.NewConfig(serviceName, lookup)
	if err != nil {
		return nil, err
	}
	return llm.New(cfg, llm.NewAnthropicAPI(apiKey, llm.AnthropicBaseURL(config.String(lookup, baseURLEnv, ""))), meter, llm.WithLogger(log))
}

type noKeyCompleter struct{}

func (noKeyCompleter) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	return llm.Response{}, errNoAPIKey
}
