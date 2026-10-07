package main

import (
	"context"
	"errors"
	"log/slog"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
)

const (
	sorobanAddrEnv = "SOROBAN_ADDR"
	apiKeyEnv      = "ANTHROPIC_API_KEY"
	baseURLEnv     = "ANTHROPIC_BASE_URL"
)

// errNoAPIKey means no model is set up: the rule score stands for every posting.
var errNoAPIKey = errors.New(apiKeyEnv + " is not set")

// scoring is the metered model client with the connection to close.
type scoring struct {
	completer app.Completer
	close     func()
}

// newScoring connects to soroban and builds the metered model client. The API
// key is required in production; elsewhere a missing key is logged and
// borderline postings keep their rule score, so the local stack still starts.
func newScoring(
	ctx context.Context, lookup config.LookupFunc, cfg config.Base, signer grpcclient.Signer, log *slog.Logger,
) (*scoring, error) {
	sorobanAddr, err := config.Required(lookup, sorobanAddrEnv)
	if err != nil {
		return nil, err
	}
	apiKey := config.String(lookup, apiKeyEnv, "")
	if apiKey == "" && cfg.IsProduction() {
		return nil, errNoAPIKey
	}
	conn, err := grpcclient.Dial(ctx, sorobanAddr, grpcclient.WithSigner(signer))
	if err != nil {
		return nil, err
	}
	closeConn := func() {
		if err := conn.Close(); err != nil {
			log.Warn("close soroban connection", slog.Any("error", err))
		}
	}
	if apiKey == "" {
		log.Warn("the model is off for scoring: " + errNoAPIKey.Error())
		return &scoring{completer: noKeyCompleter{}, close: closeConn}, nil
	}
	llmCfg, err := llm.NewConfig(cfg.Service, lookup)
	if err != nil {
		closeConn()
		return nil, err
	}
	client, err := llm.New(llmCfg, llm.NewAnthropicAPI(apiKey, llm.AnthropicBaseURL(config.String(lookup, baseURLEnv, ""))),
		sorobanv1.NewSorobanServiceClient(conn), llm.WithLogger(log))
	if err != nil {
		closeConn()
		return nil, err
	}
	return &scoring{completer: client, close: closeConn}, nil
}

// noKeyCompleter says the model is off, which scoring takes as "keep the rule
// score".
type noKeyCompleter struct{}

func (noKeyCompleter) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	return llm.Response{}, app.ErrModelOff
}
