package telemetry_test

import (
	"context"
	"log/slog"
	"net"
	"regexp"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/server"
	"github.com/0xHoaxen/shogun/pkg/telemetry"
)

const testTimeout = 5 * time.Second

var traceparentPattern = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-0[01]$`)

func TestSetupWithoutEndpointIsNoOp(t *testing.T) {
	// Arrange
	cfg := config.Base{Service: "test"}

	// Act
	shutdown, err := telemetry.Setup(context.Background(), cfg)
	// Assert
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestSetupWithEndpointShutsDownCleanly(t *testing.T) {
	// Arrange: exporters connect lazily, so an unreachable endpoint is fine.
	cfg := config.Base{Service: "test", Environment: config.EnvTest, OTLPEndpoint: "127.0.0.1:1"}
	t.Cleanup(func() { otel.SetTracerProvider(nooptrace.NewTracerProvider()) })

	// Act
	shutdown, err := telemetry.Setup(context.Background(), cfg)
	// Assert
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = shutdown(ctx) // flushing to a dead endpoint may report an error
}

func TestTraceparent(t *testing.T) {
	if got := telemetry.Traceparent(context.Background()); got != "" {
		t.Fatalf("no span: got %q, want empty", got)
	}
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	defer span.End()

	got := telemetry.Traceparent(ctx)

	if !traceparentPattern.MatchString(got) {
		t.Fatalf("traceparent %q is not W3C format", got)
	}
	if want := span.SpanContext().TraceID().String(); telemetry.TraceID(ctx) != want {
		t.Fatalf("TraceID = %q, want %q", telemetry.TraceID(ctx), want)
	}
}

func TestTraceContextPropagatesClientToServer(t *testing.T) {
	// Arrange
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(nooptrace.NewTracerProvider()) })
	if _, err := telemetry.Setup(context.Background(), config.Base{Service: "test"}); err != nil {
		t.Fatal(err)
	}

	grpcLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	cfg := config.Base{Service: "test", Environment: config.EnvTest, ShutdownTimeout: testTimeout}
	go func() {
		done <- server.Run(runCtx, cfg, slog.New(slog.DiscardHandler), func(*grpc.Server) {},
			server.WithListeners(grpcLis, httpLis))
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
	conn, err := grpcclient.Dial(context.Background(), grpcLis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	ctx, root := tp.Tracer("test").Start(ctx, "root")
	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	root.End()

	// Assert
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	var serverSpan sdktrace.ReadOnlySpan
	deadline := time.Now().Add(testTimeout)
	for serverSpan == nil && time.Now().Before(deadline) {
		for _, s := range rec.Ended() {
			if s.SpanKind() == oteltrace.SpanKindServer {
				serverSpan = s
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if serverSpan == nil {
		t.Fatal("no server span recorded")
	}
	if got, want := serverSpan.SpanContext().TraceID(), root.SpanContext().TraceID(); got != want {
		t.Fatalf("server trace id %s, want client trace id %s", got, want)
	}
}
