// Package server runs the gRPC and HTTP listeners every service exposes.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/0xHoaxen/shogun/pkg/config"
)

// Run serves gRPC (health, reflection outside production, recover and log
// interceptors) and HTTP (/healthz, /readyz, /metrics) until ctx is cancelled,
// then shuts both down within cfg.ShutdownTimeout. register adds the service's
// own gRPC handlers. A clean shutdown returns nil.
func Run(ctx context.Context, cfg config.Base, log *slog.Logger, register func(*grpc.Server), opts ...Option) error {
	s := settings{}
	for _, opt := range opts {
		s = opt(s)
	}
	grpcLis, httpLis, err := listeners(cfg, s)
	if err != nil {
		return err
	}

	grpcSrv := newGRPCServer(cfg, log, s, register)
	httpSrv := newHTTPServer(s.readiness)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		log.InfoContext(gctx, "grpc listening", slog.String("addr", grpcLis.Addr().String()))
		if err := grpcSrv.Serve(grpcLis); err != nil {
			return fmt.Errorf("server: grpc serve: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		log.InfoContext(gctx, "http listening", slog.String("addr", httpLis.Addr().String()))
		if err := httpSrv.Serve(httpLis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server: http serve: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		return shutdown(log, cfg.ShutdownTimeout, grpcSrv, httpSrv)
	})
	return g.Wait()
}

func newGRPCServer(cfg config.Base, log *slog.Logger, s settings, register func(*grpc.Server)) *grpc.Server {
	unary := []grpc.UnaryServerInterceptor{recoverUnary(log), logUnary(log)}
	stream := []grpc.StreamServerInterceptor{recoverStream(log), logStream(log)}
	if s.authUnary != nil {
		unary = append(unary, s.authUnary)
	}
	if s.authStream != nil {
		stream = append(stream, s.authStream)
	}
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(stream...),
	)
	healthpb.RegisterHealthServer(srv, health.NewServer())
	if !cfg.IsProduction() {
		reflection.Register(srv)
	}
	if register != nil {
		register(srv)
	}
	return srv
}

func listeners(cfg config.Base, s settings) (grpcLis, httpLis net.Listener, err error) {
	if s.grpcListener != nil && s.httpListener != nil {
		return s.grpcListener, s.httpListener, nil
	}
	grpcLis, err = net.Listen("tcp", ":"+strconv.Itoa(cfg.GRPCPort))
	if err != nil {
		return nil, nil, fmt.Errorf("server: listen grpc: %w", err)
	}
	httpLis, err = net.Listen("tcp", ":"+strconv.Itoa(cfg.HTTPPort))
	if err != nil {
		_ = grpcLis.Close()
		return nil, nil, fmt.Errorf("server: listen http: %w", err)
	}
	return grpcLis, httpLis, nil
}

// shutdown stops both servers gracefully, forcing a stop when timeout expires.
func shutdown(log *slog.Logger, timeout time.Duration, grpcSrv *grpc.Server, httpSrv *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Info("shutting down", slog.Duration("timeout", timeout))
	grpcDone := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop()
		close(grpcDone)
	}()

	var errs []error
	if err := httpSrv.Shutdown(ctx); err != nil {
		_ = httpSrv.Close()
		errs = append(errs, fmt.Errorf("server: http shutdown: %w", err))
	}
	select {
	case <-grpcDone:
	case <-ctx.Done():
		grpcSrv.Stop()
		<-grpcDone
		errs = append(errs, errors.New("server: grpc graceful stop timed out"))
	}
	return errors.Join(errs...)
}
