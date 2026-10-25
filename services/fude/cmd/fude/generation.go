package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/jobs"
	dojosource "github.com/0xHoaxen/shogun/services/fude/internal/transport/dojo"
	kagamisource "github.com/0xHoaxen/shogun/services/fude/internal/transport/kagami"
	tsubamemailer "github.com/0xHoaxen/shogun/services/fude/internal/transport/tsubame"
)

const (
	tsubameAddrEnv = "TSUBAME_ADDR"
	sorobanAddrEnv = "SOROBAN_ADDR"
	kagamiAddrEnv  = "KAGAMI_ADDR"
	dojoAddrEnv    = "DOJO_ADDR"
)

// generation is what draft generation needs wired: the River worker for the
// relay's client, the queue the use cases insert through, and a function that
// releases the connections.
type generation struct {
	setup  jobs.Setup
	queue  *jobs.RiverQueue
	mailer app.Mailer
	tester *app.ProviderTester
	close  func()
}

// newGeneration connects to soroban and kagami and builds the generator. The
// model is each user's own: soroban hands back their provider when it reserves a
// call, and a user with none gets a draft that fails with a clear reason.
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
	dojoAddr, err := config.Required(lookup, dojoAddrEnv)
	if err != nil {
		return nil, err
	}
	tsubameAddr, err := config.Required(lookup, tsubameAddrEnv)
	if err != nil {
		return nil, err
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
	dojoConn, err := grpcclient.Dial(ctx, dojoAddr, grpcclient.WithSigner(signer))
	if err != nil {
		_ = sorobanConn.Close()
		_ = kagamiConn.Close()
		return nil, fmt.Errorf("dial dojo: %w", err)
	}
	tsubameConn, err := grpcclient.Dial(ctx, tsubameAddr, grpcclient.WithSigner(signer))
	if err != nil {
		_ = sorobanConn.Close()
		_ = kagamiConn.Close()
		_ = dojoConn.Close()
		return nil, fmt.Errorf("dial tsubame: %w", err)
	}
	closeConns := func() {
		_ = sorobanConn.Close()
		_ = kagamiConn.Close()
		_ = dojoConn.Close()
		_ = tsubameConn.Close()
	}

	completer, err := newCompleter(cfg, sorobanv1.NewSorobanServiceClient(sorobanConn), log)
	if err != nil {
		closeConns()
		return nil, err
	}
	source := dojosource.New(dojov1.NewDojoServiceClient(dojoConn), kagamisource.New(kagamiv1.NewKagamiServiceClient(kagamiConn)))
	generator := app.NewGenerator(pool, completer, source, log, nil, app.WithEmbedder(embedder))
	var sampleEmbedder jobs.SampleEmbedder
	if embedder != nil {
		sampleEmbedder = app.NewVoiceEmbedder(pool, embedder)
	}
	return &generation{
		queue: queue, close: closeConns, setup: jobs.NewSetup(generator, sampleEmbedder, log),
		mailer: tsubamemailer.New(tsubamev1.NewTsubameServiceClient(tsubameConn)),
		tester: app.NewProviderTester(completer, testerOptions(cfg)...),
	}, nil
}

// newCompleter returns the metered model client. It holds no credentials: every
// call is made with the provider of the user it is for.
func newCompleter(cfg config.Base, meter llm.Meter, log *slog.Logger) (*llm.Client, error) {
	llmCfg, err := llm.NewConfig(serviceName)
	if err != nil {
		return nil, err
	}
	return llm.New(llmCfg, llm.NewOpenAIAPI(llm.ForEnvironment(cfg.Environment)...), meter, llm.WithLogger(log))
}

// testerOptions lets a tested address be plain http where private addresses are
// allowed too, which is local development and tests.
func testerOptions(cfg config.Base) []app.TesterOption {
	if cfg.Environment == config.EnvLocal || cfg.Environment == config.EnvTest {
		return []app.TesterOption{app.AllowInsecureAddresses()}
	}
	return nil
}
