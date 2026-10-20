package server

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/internaltls/internaltlstest"
)

// useTLS points the process at a certificate for host issued by ca, as a
// service's environment would.
func useTLS(t *testing.T, ca internaltlstest.CA, host string) {
	t.Helper()
	for k, v := range ca.Issue(t, t.TempDir(), host, time.Now().Add(time.Hour)) {
		t.Setenv(k, v)
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lis.Close() })
	return lis
}

func checkHealth(t *testing.T, conn *grpc.ClientConn) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	_, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	return err
}

func TestServerWithTLSServesClientsOfTheSameCAAndRefusesTheRest(t *testing.T) {
	// Arrange: the server and the client are two services with certificates from one CA.
	ca := internaltlstest.NewCA(t, "ca")
	useTLS(t, ca, "127.0.0.1")
	r := start(t, config.Base{Environment: config.EnvLocal}, nil)
	defer r.stop(t)

	// Act
	trusted, err := grpcclient.Dial(context.Background(), r.grpcAddr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = trusted.Close() }()
	plaintext, err := grpc.NewClient(r.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = plaintext.Close() }()

	// Assert
	if err := checkHealth(t, trusted); err != nil {
		t.Fatalf("a client with a certificate from the same CA was refused: %v", err)
	}
	if err := checkHealth(t, plaintext); err == nil {
		t.Fatal("the server answered a plaintext client")
	}
}

func TestServerRefusesAClientWhoseCertificateCameFromAnotherCA(t *testing.T) {
	ours, theirs := internaltlstest.NewCA(t, "ours"), internaltlstest.NewCA(t, "theirs")
	useTLS(t, ours, "127.0.0.1")
	r := start(t, config.Base{Environment: config.EnvLocal}, nil)
	defer r.stop(t)
	// The caller trusts our CA to check the server but presents a certificate we did not issue.
	stranger := theirs.Issue(t, t.TempDir(), "stranger", time.Now().Add(time.Hour))
	t.Setenv("INTERNAL_TLS_CERT_FILE", stranger["INTERNAL_TLS_CERT_FILE"])
	t.Setenv("INTERNAL_TLS_KEY_FILE", stranger["INTERNAL_TLS_KEY_FILE"])
	conn, err := grpcclient.Dial(context.Background(), r.grpcAddr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := checkHealth(t, conn); err == nil {
		t.Fatal("the server answered a client certificate from another CA")
	}
}

func TestRunRefusesToStartInProductionWithoutCertificates(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	grpcLis, httpLis := listen(t), listen(t)

	err := Run(context.Background(), config.Base{Environment: config.EnvProduction, DatabaseURL: "x", ShutdownTimeout: testTimeout},
		nil, nil, WithListeners(grpcLis, httpLis))

	if err == nil {
		t.Fatal("Run started a production server without TLS")
	}
}
