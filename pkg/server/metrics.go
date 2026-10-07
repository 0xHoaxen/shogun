package server

import (
	"context"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

const healthPrefix = "/grpc.health.v1.Health/"

// RED metrics for every RPC, served on /metrics. A call counts under the code
// the handler returned, or "OK".
var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "shogun_grpc_requests_total",
		Help: "gRPC calls handled, by full method and status code.",
	}, []string{"method", "code"})

	requestSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "shogun_grpc_request_duration_seconds",
		Help:    "How long gRPC calls took, by full method. A stream counts until it ends.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})
)

// metricsUnary counts and times each unary call. It sits outermost, so it sees
// the code recover and the auth interceptor produce.
func metricsUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := next(ctx, req)
		observe(info.FullMethod, err, start)
		return resp, err
	}
}

// metricsStream is the streaming counterpart of metricsUnary.
func metricsStream() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		start := time.Now()
		err := next(srv, ss)
		observe(info.FullMethod, err, start)
		return err
	}
}

// observe records one finished call. Health probes are left out so they do not
// dilute the error rate.
func observe(method string, err error, start time.Time) {
	if strings.HasPrefix(method, healthPrefix) {
		return
	}
	requestsTotal.WithLabelValues(method, status.Code(err).String()).Inc()
	requestSeconds.WithLabelValues(method).Observe(time.Since(start).Seconds())
}
