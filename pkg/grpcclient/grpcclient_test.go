package grpcclient

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/0xHoaxen/shogun/pkg/authz"
)

type pinger interface{}

func unaryDesc(handler func(context.Context) (any, error)) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: "test.Ping",
		HandlerType: (*pinger)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Ping",
			Handler: func(_ any, ctx context.Context, dec func(any) error, ic grpc.UnaryServerInterceptor) (any, error) {
				in := new(emptypb.Empty)
				if err := dec(in); err != nil {
					return nil, err
				}
				h := func(ctx context.Context, _ any) (any, error) { return handler(ctx) }
				if ic == nil {
					return h(ctx, in)
				}
				return ic(ctx, in, &grpc.UnaryServerInfo{FullMethod: "/test.Ping/Ping"}, h)
			},
		}},
		Streams: []grpc.StreamDesc{{
			StreamName: "Watch", ServerStreams: true,
			Handler: func(_ any, ss grpc.ServerStream) error {
				out, err := handler(ss.Context())
				if err != nil {
					return err
				}
				return ss.SendMsg(out)
			},
		}},
	}
}

func startBufServer(t *testing.T, a *authz.Authority, handler func(context.Context) (any, error)) func(context.Context, string) (net.Conn, error) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	var opts []grpc.ServerOption
	if a != nil {
		opts = append(opts, grpc.UnaryInterceptor(a.UnaryServerInterceptor()), grpc.StreamInterceptor(a.StreamServerInterceptor()))
	}
	srv := grpc.NewServer(opts...)
	srv.RegisterService(unaryDesc(handler), struct{}{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return func(context.Context, string) (net.Conn, error) { return lis.Dial() }
}

func echoOwner(ctx context.Context) (any, error) {
	id, ok := authz.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Internal, "no identity")
	}
	return wrapperspb.String(id.OwnerID + "/" + id.RequestID), nil
}

func newAuthority(t *testing.T) *authz.Authority {
	t.Helper()
	a, err := authz.New([]byte(strings.Repeat("s", authz.MinKeyLength)))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func dial(t *testing.T, dialer func(context.Context, string) (net.Conn, error), opts ...Option) *grpc.ClientConn {
	t.Helper()
	all := append([]Option{WithDialOptions(grpc.WithContextDialer(dialer))}, opts...)
	conn, err := Dial(context.Background(), "passthrough:///bufnet", all...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestIdentityPropagatesEndToEnd(t *testing.T) {
	a := newAuthority(t)
	conn := dial(t, startBufServer(t, a, echoOwner), WithSigner(a))
	ctx := authz.WithIdentity(context.Background(), authz.Identity{OwnerID: "owner-9", RequestID: "req-9"})

	out := new(wrapperspb.StringValue)
	if err := conn.Invoke(ctx, "/test.Ping/Ping", new(emptypb.Empty), out); err != nil {
		t.Fatalf("unary: %v", err)
	}
	if out.GetValue() != "owner-9/req-9" {
		t.Fatalf("unary handler saw %q", out.GetValue())
	}

	cs, err := conn.NewStream(ctx, &grpc.StreamDesc{StreamName: "Watch", ServerStreams: true}, "/test.Ping/Watch")
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.SendMsg(new(emptypb.Empty)); err != nil {
		t.Fatal(err)
	}
	_ = cs.CloseSend()
	sout := new(wrapperspb.StringValue)
	if err := cs.RecvMsg(sout); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if sout.GetValue() != "owner-9/req-9" {
		t.Fatalf("stream handler saw %q", sout.GetValue())
	}
}

func TestNoIdentityInContextIsRejected(t *testing.T) {
	a := newAuthority(t)
	conn := dial(t, startBufServer(t, a, echoOwner), WithSigner(a))
	err := conn.Invoke(context.Background(), "/test.Ping/Ping", new(emptypb.Empty), new(wrapperspb.StringValue))
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestDefaultDeadlineApplied(t *testing.T) {
	var hadDeadline atomic.Bool
	handler := func(ctx context.Context) (any, error) {
		_, ok := ctx.Deadline()
		hadDeadline.Store(ok)
		return wrapperspb.String("ok"), nil
	}
	conn := dial(t, startBufServer(t, nil, handler))
	if err := conn.Invoke(context.Background(), "/test.Ping/Ping", new(emptypb.Empty), new(wrapperspb.StringValue)); err != nil {
		t.Fatal(err)
	}
	if !hadDeadline.Load() {
		t.Fatal("server saw no deadline")
	}
}

func TestDefaultDeadlineExpires(t *testing.T) {
	handler := func(ctx context.Context) (any, error) {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	conn := dial(t, startBufServer(t, nil, handler), WithTimeout(50*time.Millisecond))
	err := conn.Invoke(context.Background(), "/test.Ping/Ping", new(emptypb.Empty), new(wrapperspb.StringValue))
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("code = %v, want DeadlineExceeded", status.Code(err))
	}
}

func TestRetriesOnlyUnavailable(t *testing.T) {
	tests := []struct {
		name      string
		code      codes.Code
		wantCalls int32
	}{
		{"unavailable retried", codes.Unavailable, 3},
		{"internal not retried", codes.Internal, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			handler := func(context.Context) (any, error) {
				calls.Add(1)
				return nil, status.Error(tt.code, "boom")
			}
			conn := dial(t, startBufServer(t, nil, handler))
			err := conn.Invoke(context.Background(), "/test.Ping/Ping", new(emptypb.Empty), new(wrapperspb.StringValue))
			if status.Code(err) != tt.code {
				t.Fatalf("code = %v, want %v", status.Code(err), tt.code)
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Fatalf("handler calls = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}
