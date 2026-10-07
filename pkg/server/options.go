package server

import (
	"context"
	"net"
	"slices"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
)

// ReadinessCheck reports an error when a dependency is not ready to serve.
type ReadinessCheck func(ctx context.Context) error

type settings struct {
	grpcListener net.Listener
	httpListener net.Listener
	readiness    []ReadinessCheck
	authUnary    grpc.UnaryServerInterceptor
	authStream   grpc.StreamServerInterceptor
	collectors   []prometheus.Collector
}

// Option customises Run. Options are pure: they return a modified copy.
type Option func(settings) settings

// WithListeners makes Run serve on the given listeners instead of the ports in
// config. Tests pass listeners bound to ":0".
func WithListeners(grpcListener, httpListener net.Listener) Option {
	return func(s settings) settings {
		s.grpcListener = grpcListener
		s.httpListener = httpListener
		return s
	}
}

// WithReadinessCheck adds a check that /readyz runs on every request.
func WithReadinessCheck(check ReadinessCheck) Option {
	return func(s settings) settings {
		s.readiness = append(slices.Clone(s.readiness), check)
		return s
	}
}

// WithAuth installs identity-checking interceptors (for example from
// authz.Authority) after recover and log. Off by default: torii mints identity
// and must not require it.
func WithAuth(unary grpc.UnaryServerInterceptor, stream grpc.StreamServerInterceptor) Option {
	return func(s settings) settings {
		s.authUnary = unary
		s.authStream = stream
		return s
	}
}

// WithCollector serves a service's own metrics on /metrics for as long as Run
// is serving. Run registers it on the default registry and unregisters it on
// the way out; a registration that fails is logged and does not stop the service.
func WithCollector(c prometheus.Collector) Option {
	return func(s settings) settings {
		s.collectors = append(slices.Clone(s.collectors), c)
		return s
	}
}
