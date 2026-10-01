package authz

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type pinger interface{}

// pingDesc is a hand-written service: Ping echoes the caller's owner id.
var pingDesc = grpc.ServiceDesc{
	ServiceName: "test.Ping",
	HandlerType: (*pinger)(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "Ping",
		Handler: func(_ any, ctx context.Context, dec func(any) error, ic grpc.UnaryServerInterceptor) (any, error) {
			in := new(emptypb.Empty)
			if err := dec(in); err != nil {
				return nil, err
			}
			h := func(ctx context.Context, _ any) (any, error) {
				id, ok := FromContext(ctx)
				if !ok {
					return nil, status.Error(codes.Internal, "no identity in handler")
				}
				return wrapperspb.String(id.OwnerID + "/" + id.RequestID), nil
			}
			if ic == nil {
				return h(ctx, in)
			}
			return ic(ctx, in, &grpc.UnaryServerInfo{FullMethod: "/test.Ping/Ping"}, h)
		},
	}},
	Streams: []grpc.StreamDesc{{
		StreamName:    "Watch",
		ServerStreams: true,
		Handler: func(_ any, ss grpc.ServerStream) error {
			id, ok := FromContext(ss.Context())
			if !ok {
				return status.Error(codes.Internal, "no identity in stream")
			}
			return ss.SendMsg(wrapperspb.String(id.OwnerID))
		},
	}},
}

func startServer(t *testing.T, a *Authority) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnaryInterceptor(a.UnaryServerInterceptor()), grpc.StreamInterceptor(a.StreamServerInterceptor()))
	srv.RegisterService(&pingDesc, struct{}{})
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestInterceptors(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	server, _ := New(testKey, fixedClock(now))
	conn := startServer(t, server)
	stale, _ := New(testKey, fixedClock(now.Add(-2*TokenTTL)))
	wrongKey, _ := New([]byte("0123456789abcdef0123456789abcdef"), fixedClock(now))

	tok := func(a *Authority) string {
		s, err := a.Sign(Identity{OwnerID: "owner-1", RequestID: "req-1"})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	tests := []struct {
		name   string
		header []string
		want   codes.Code
	}{
		{"missing", nil, codes.Unauthenticated},
		{"garbage", []string{"garbage"}, codes.Unauthenticated},
		{"expired", []string{tok(stale)}, codes.Unauthenticated},
		{"wrong key", []string{tok(wrongKey)}, codes.Unauthenticated},
		{"duplicate headers", []string{tok(server), tok(server)}, codes.Unauthenticated},
		{"valid", []string{tok(server)}, codes.OK},
	}
	for _, tt := range tests {
		t.Run("unary "+tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for _, h := range tt.header {
				ctx = metadata.AppendToOutgoingContext(ctx, Header, h)
			}
			out := new(wrapperspb.StringValue)
			err := conn.Invoke(ctx, "/test.Ping/Ping", new(emptypb.Empty), out)
			if status.Code(err) != tt.want {
				t.Fatalf("code = %v (%v), want %v", status.Code(err), err, tt.want)
			}
			if tt.want == codes.OK && out.GetValue() != "owner-1/req-1" {
				t.Fatalf("handler saw %q", out.GetValue())
			}
		})
		t.Run("stream "+tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for _, h := range tt.header {
				ctx = metadata.AppendToOutgoingContext(ctx, Header, h)
			}
			cs, err := conn.NewStream(ctx, &pingDesc.Streams[0], "/test.Ping/Watch")
			if err == nil {
				err = cs.SendMsg(new(emptypb.Empty))
			}
			if err == nil {
				err = cs.CloseSend()
			}
			out := new(wrapperspb.StringValue)
			if err == nil {
				err = cs.RecvMsg(out)
			}
			if status.Code(err) != tt.want {
				t.Fatalf("code = %v (%v), want %v", status.Code(err), err, tt.want)
			}
			if tt.want == codes.OK && out.GetValue() != "owner-1" {
				t.Fatalf("stream handler saw %q", out.GetValue())
			}
		})
	}
}

func TestHealthIsExempt(t *testing.T) {
	a, _ := New(testKey)
	conn := startServer(t, a)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("health check without token: %v", err)
	}
}
