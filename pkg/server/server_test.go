package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/pkg/config"
)

const testTimeout = 5 * time.Second

type running struct {
	grpcAddr string
	httpAddr string
	cancel   context.CancelFunc
	done     chan error
}

func start(t *testing.T, cfg config.Base, register func(*grpc.Server), opts ...Option) running {
	t.Helper()
	grpcLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ShutdownTimeout = testTimeout
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.DiscardHandler)
	done := make(chan error, 1)
	all := append([]Option{WithListeners(grpcLis, httpLis)}, opts...)
	go func() { done <- Run(ctx, cfg, log, register, all...) }()
	return running{grpcAddr: grpcLis.Addr().String(), httpAddr: httpLis.Addr().String(), cancel: cancel, done: done}
}

func (r running) stop(t *testing.T) {
	t.Helper()
	r.cancel()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Run did not exit within the shutdown timeout")
	}
}

func dial(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func get(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func TestRunServesHealthAndShutsDownCleanly(t *testing.T) {
	r := start(t, config.Base{Environment: config.EnvLocal}, nil)

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	resp, err := grpc_health_v1.NewHealthClient(dial(t, r.grpcAddr)).Check(ctx, &grpc_health_v1.HealthCheckRequest{}, grpc.WaitForReady(true))
	if err != nil || resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("grpc health = %v, %v", resp.GetStatus(), err)
	}
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		if got := get(t, "http://"+r.httpAddr+path); got != http.StatusOK {
			t.Errorf("GET %s = %d", path, got)
		}
	}
	r.stop(t)
}

func TestReadinessCheckFailure(t *testing.T) {
	failing := func(context.Context) error { return io.ErrUnexpectedEOF }
	r := start(t, config.Base{}, nil, WithReadinessCheck(failing))
	if got := get(t, "http://"+r.httpAddr+"/readyz"); got != http.StatusServiceUnavailable {
		t.Fatalf("readyz = %d, want 503", got)
	}
	if got := get(t, "http://"+r.httpAddr+"/healthz"); got != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", got)
	}
	r.stop(t)
}

func TestPanickingHandlerReturnsInternal(t *testing.T) {
	// Registering over the built-in health service would panic, so use a
	// distinct service name by replacing the registration through a wrapper.
	register := func(s *grpc.Server) {
		s.RegisterService(&grpc.ServiceDesc{
			ServiceName: "test.Panic",
			HandlerType: (*any)(nil),
			Methods: []grpc.MethodDesc{{
				MethodName: "Boom",
				Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
					in := &grpc_health_v1.HealthCheckRequest{}
					if err := dec(in); err != nil {
						return nil, err
					}
					boom := func(context.Context, any) (any, error) { panic("boom") }
					return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/test.Panic/Boom"}, boom)
				},
			}},
		}, struct{}{})
	}
	r := start(t, config.Base{}, register)
	conn := dial(t, r.grpcAddr)

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	err := conn.Invoke(ctx, "/test.Panic/Boom", &grpc_health_v1.HealthCheckRequest{}, &grpc_health_v1.HealthCheckResponse{}, grpc.WaitForReady(true))
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v (%v), want Internal", status.Code(err), err)
	}
	r.stop(t)
}

func TestReflectionOffInProduction(t *testing.T) {
	tests := []struct {
		env  string
		want codes.Code
	}{
		{config.EnvLocal, codes.OK},
		{config.EnvProduction, codes.Unimplemented},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			r := start(t, config.Base{Environment: tt.env}, nil)
			conn := dial(t, r.grpcAddr)
			ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
			defer cancel()
			err := listServices(ctx, conn)
			got := status.Code(err)
			if got != tt.want {
				t.Fatalf("code = %v (%v), want %v", got, err, tt.want)
			}
			r.stop(t)
		})
	}
}

func TestRunFailsWhenPortBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	port := busy.Addr().(*net.TCPAddr).Port
	cfg := config.Base{GRPCPort: port, HTTPPort: port, ShutdownTimeout: time.Second}
	log := slog.New(slog.DiscardHandler)
	if err := Run(t.Context(), cfg, log, nil); err == nil {
		t.Fatal("expected listen error")
	}
}

// listServices asks the reflection service for its service list.
func listServices(ctx context.Context, conn *grpc.ClientConn) error {
	ctx, cancel := context.WithCancel(ctx) // ends the stream so shutdown is not held up
	defer cancel()
	stream, err := reflectionpb.NewServerReflectionClient(conn).ServerReflectionInfo(ctx, grpc.WaitForReady(true))
	if err != nil {
		return err
	}
	req := &reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{ListServices: ""},
	}
	if err := stream.Send(req); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	_, err = stream.Recv()
	return err
}
