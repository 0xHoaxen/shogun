package main

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/pkg/server"
)

const (
	testIdentityKey = "0123456789abcdef0123456789abcdef"
	startupTimeout  = 30 * time.Second
	exitTimeout     = 15 * time.Second
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func TestRunServesHealthAndStopsOnCancel(t *testing.T) {
	// Arrange
	env := map[string]string{
		"ENVIRONMENT":          "test",
		"DATABASE_URL":         postgrestest.NewDatabase(t),
		"IDENTITY_SIGNING_KEY": testIdentityKey,
	}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	grpcLis := listen(t)
	httpLis := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	// Act
	go func() { done <- run(ctx, lookup, server.WithListeners(grpcLis, httpLis)) }()
	status := checkHealth(t, grpcLis.Addr().String())
	cancel()

	// Assert
	if status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("health status = %v, want SERVING", status)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(exitTimeout):
		t.Fatal("run did not exit after cancel")
	}
}

func TestRunRejectsMissingIdentityKey(t *testing.T) {
	// Arrange
	env := map[string]string{"ENVIRONMENT": "test", "DATABASE_URL": "postgres://unused"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	// Act
	err := run(context.Background(), lookup)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "IDENTITY_SIGNING_KEY") {
		t.Fatalf("err = %v, want mention of IDENTITY_SIGNING_KEY", err)
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return lis
}

func checkHealth(t *testing.T, addr string) grpc_health_v1.HealthCheckResponse_ServingStatus {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := grpc_health_v1.NewHealthClient(conn)
	deadline := time.Now().Add(startupTimeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		cancel()
		if err == nil {
			return resp.GetStatus()
		}
		if time.Now().After(deadline) {
			t.Fatalf("health check never succeeded: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
