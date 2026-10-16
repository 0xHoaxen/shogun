package main

import (
	"context"
	"log/slog"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/app"
)

const (
	sorobanAddrEnv = "SOROBAN_ADDR"
)

// scoring is the metered model client with the connection to close.
type scoring struct {
	completer app.Completer
	close     func()
}

// newScoring connects to soroban and builds the metered model client. The model
// is each user's own: soroban hands back their provider when it reserves a call,
// and a user with none keeps the rule score on borderline postings.
func newScoring(
	ctx context.Context, lookup config.LookupFunc, cfg config.Base, signer grpcclient.Signer, log *slog.Logger,
) (*scoring, error) {
	sorobanAddr, err := config.Required(lookup, sorobanAddrEnv)
	if err != nil {
		return nil, err
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
	llmCfg, err := llm.NewConfig(cfg.Service)
	if err != nil {
		closeConn()
		return nil, err
	}
	client, err := llm.New(llmCfg, llm.NewAnthropicAPI(llm.ForEnvironment(cfg.Environment)...),
		sorobanv1.NewSorobanServiceClient(conn), llm.WithLogger(log))
	if err != nil {
		closeConn()
		return nil, err
	}
	return &scoring{completer: client, close: closeConn}, nil
}
