package app_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/llm/llmtest"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
)

// realTester is a ProviderTester over the real HTTP client, with its address
// guard on, and a fake meter standing in for soroban.
func realTester(t *testing.T, meter *llmtest.Meter, logs *bytes.Buffer) *app.ProviderTester {
	t.Helper()
	cfg, err := llm.NewConfig("fude")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	client, err := llm.New(cfg, llm.NewOpenAIAPI(llm.MaxRetries(0)), meter, llm.WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return app.NewProviderTester(client)
}

func TestTestDetailsNeverCallsAPrivateOrLocalAddress(t *testing.T) {
	for name, address := range map[string]string{
		"localhost":         "https://localhost/v1",
		"loopback":          "https://127.0.0.1/v1",
		"loopback IPv6":     "https://[::1]/v1",
		"private 10.x":      "https://10.0.0.5/v1",
		"private 192.168":   "https://192.168.1.10/v1",
		"private 172.16":    "https://172.16.0.1/v1",
		"link-local":        "https://169.254.169.254/latest",
		"carrier-grade":     "https://100.64.0.1/v1",
		"unspecified":       "https://0.0.0.0/v1",
		"mapped loopback":   "https://[::ffff:127.0.0.1]/v1",
		"private by a port": "https://10.1.2.3:8443/v1",
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			var logs bytes.Buffer
			meter := &llmtest.Meter{}
			ctx := authz.WithIdentity(context.Background(), authz.Identity{OwnerID: "owner-1", RequestID: "r"})

			// Act
			res, err := realTester(t, meter, &logs).TestDetails(ctx, details(address))

			// Assert
			if err != nil || res.OK || res.Failure != llm.FailureNotPublic {
				t.Fatalf("res %+v, err %v; want %q", res, err, llm.FailureNotPublic)
			}
			if len(meter.Released) != 1 || len(meter.Committed) != 0 {
				t.Fatalf("released %d, committed %d; want the hold given back", len(meter.Released), len(meter.Committed))
			}
			for _, seen := range []string{fmt.Sprintf("%+v", res), logs.String(), fmt.Sprint(meter.Reserved)} {
				if strings.Contains(seen, typedKey) {
					t.Fatalf("the key leaked into %q", seen)
				}
			}
		})
	}
}

func TestTestDetailsTellsSorobanTheModelAndNeverTheKeyOrAddress(t *testing.T) {
	var logs bytes.Buffer
	meter := &llmtest.Meter{}
	ctx := authz.WithIdentity(context.Background(), authz.Identity{OwnerID: "owner-1", RequestID: "r"})

	_, _ = realTester(t, meter, &logs).TestDetails(ctx, details("https://10.9.9.9/secret-path"))

	if len(meter.Reserved) != 1 {
		t.Fatalf("reserved %d, want 1", len(meter.Reserved))
	}
	got := meter.Reserved[0]
	if got.GetUnsavedModel() != "my-llama-70b" || got.GetFeature() != "fude.test_connection" {
		t.Fatalf("reserved %v, want the model name on the test feature", got)
	}
	if s := got.String(); strings.Contains(s, typedKey) || strings.Contains(s, "secret-path") || strings.Contains(s, "10.9.9.9") {
		t.Fatalf("soroban was told more than the model name: %v", got)
	}
}
