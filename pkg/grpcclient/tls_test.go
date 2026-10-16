package grpcclient_test

import (
	"context"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/grpcclient"
)

func TestDialRefusesToConnectInProductionWithoutCertificates(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")

	conn, err := grpcclient.Dial(context.Background(), "soroban:9090")

	if err == nil {
		_ = conn.Close()
		t.Fatal("Dial returned a plaintext connection in production")
	}
}

func TestDialIsPlaintextInLocalDevelopment(t *testing.T) {
	t.Setenv("ENVIRONMENT", "local")

	conn, err := grpcclient.Dial(context.Background(), "localhost:9090")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = conn.Close()
}
