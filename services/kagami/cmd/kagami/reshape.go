package main

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
)

const sorobanAddrEnv = "SOROBAN_ADDR"

// reshaper is what AI import needs, with the connection to close.
type reshaper struct {
	option app.Option
	close  func()
}

// newReshaper connects to soroban and builds the metered model client that maps
// imported sheets onto the import columns. It holds no credentials: soroban
// hands back the calling user's own provider when it reserves each call.
func newReshaper(
	ctx context.Context, lookup config.LookupFunc, cfg config.Base, signer grpcclient.Signer, log *slog.Logger,
) (*reshaper, error) {
	sorobanAddr, err := config.Required(lookup, sorobanAddrEnv)
	if err != nil {
		return nil, err
	}
	conn, err := grpcclient.Dial(ctx, sorobanAddr, grpcclient.WithSigner(signer))
	if err != nil {
		return nil, err
	}
	llmCfg, err := llm.NewConfig(cfg.Service)
	if err != nil {
		closeConn(conn, log)
		return nil, err
	}
	meter := sorobanv1.NewSorobanServiceClient(conn)
	completer, err := llm.New(llmCfg, llm.NewOpenAIAPI(llm.ForEnvironment(cfg.Environment)...), meter, llm.WithLogger(log))
	if err != nil {
		closeConn(conn, log)
		return nil, err
	}
	return &reshaper{
		option: app.WithReshaper(completer),
		close:  func() { closeConn(conn, log) },
	}, nil
}

func closeConn(conn *grpc.ClientConn, log *slog.Logger) {
	if err := conn.Close(); err != nil {
		log.Warn("close soroban connection", slog.Any("error", err))
	}
}
