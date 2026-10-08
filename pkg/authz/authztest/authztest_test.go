package authztest

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
)

const sinkMethod = "shogun.events.v1.EventSinkService.Deliver"

// sink answers every delivery, so a call that gets through authz succeeds.
type sink struct {
	eventsv1.UnimplementedEventSinkServiceServer
}

func (sink) Deliver(context.Context, *eventsv1.DeliverRequest) (*eventsv1.DeliverResponse, error) {
	return &eventsv1.DeliverResponse{}, nil
}

// serve runs an EventSinkService, with or without the auth interceptors, and
// returns a connection to it.
func serve(t *testing.T, withAuth bool) *grpc.ClientConn {
	t.Helper()
	var opts []grpc.ServerOption
	if withAuth {
		authority, err := authz.New([]byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatal(err)
		}
		opts = append(opts, grpc.UnaryInterceptor(authority.UnaryServerInterceptor()), grpc.StreamInterceptor(authority.StreamServerInterceptor()))
	}
	srv := grpc.NewServer(opts...)
	eventsv1.RegisterEventSinkServiceServer(srv, sink{})
	reflection.Register(srv)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestSweepFindsEveryMethodAndSkipsProbesAndReflection(t *testing.T) {
	// Act
	results, err := sweep(serve(t, true))
	// Assert
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(results) != 1 || results[0].method != sinkMethod {
		t.Fatalf("results = %+v, want only %s", results, sinkMethod)
	}
}

func TestSweepSeesUnauthenticatedWhereTheServerEnforcesAuth(t *testing.T) {
	// Act
	results, err := sweep(serve(t, true))
	// Assert
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if code := status.Code(results[0].err); code != codes.Unauthenticated {
		t.Errorf("%s answered %v, want Unauthenticated", results[0].method, code)
	}
}

func TestSweepSeesAnOpenMethodWhereTheServerDoesNotEnforceAuth(t *testing.T) {
	// Act
	results, err := sweep(serve(t, false))
	// Assert: this is what makes RequireIdentityOnEveryRPC fail on such a server.
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(results) != 1 || results[0].err != nil {
		t.Errorf("results = %+v, want one method that answered without error", results)
	}
}

func TestRequireIdentityOnEveryRPCPassesOnAServerThatEnforcesAuth(t *testing.T) {
	// Arrange
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := authz.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.UnaryInterceptor(authority.UnaryServerInterceptor()), grpc.StreamInterceptor(authority.StreamServerInterceptor()))
	eventsv1.RegisterEventSinkServiceServer(srv, sink{})
	reflection.Register(srv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	// Act and assert
	RequireIdentityOnEveryRPC(t, lis.Addr().String())
}
