package grpc_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/netguard"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
)

// fakeCompleter answers a connection test with a fixed result.
type fakeCompleter struct {
	resp llm.Response
	err  error
}

func (f fakeCompleter) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	return f.resp, f.err
}

func (f fakeCompleter) CompleteWith(context.Context, string, llm.Access, llm.Request) (llm.Response, error) {
	return f.resp, f.err
}

// errorInfo returns the ErrorInfo detail of err, or nil.
func errorInfo(err error) *errdetails.ErrorInfo {
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	return nil
}

func testerOf(resp llm.Response, err error) *app.ProviderTester {
	return app.NewProviderTester(fakeCompleter{resp: resp, err: err})
}

func TestTestProviderReportsAWorkingProvider(t *testing.T) {
	h := newHarnessWith(t, testerOf(llm.Response{Text: "ok", Model: "claude-opus-5-5"}, nil))

	res, err := h.client.TestProvider(h.ctx(t), &fudev1.TestProviderRequest{})

	if err != nil || !res.GetOk() || res.GetModel() != "claude-opus-5-5" || res.GetFailure() != fudev1.ProviderFailure_PROVIDER_FAILURE_UNSPECIFIED {
		t.Fatalf("res %v, err %v; want a working provider", res, err)
	}
}

func TestTestProviderNamesWhyAProviderFailedWithoutItsOwnWords(t *testing.T) {
	h := newHarnessWith(t, testerOf(llm.Response{}, fmt.Errorf("claude call: %w", netguard.ErrNotPublic)))

	res, err := h.client.TestProvider(h.ctx(t), &fudev1.TestProviderRequest{})

	if err != nil || res.GetOk() || res.GetFailure() != fudev1.ProviderFailure_PROVIDER_FAILURE_NOT_PUBLIC {
		t.Fatalf("res %v, err %v; want ok=false with the failure not_public", res, err)
	}
}

func TestTestProviderForAUserWithoutOneSaysSo(t *testing.T) {
	h := newHarnessWith(t, testerOf(llm.Response{}, llm.ErrProviderNotConfigured))

	_, err := h.client.TestProvider(h.ctx(t), &fudev1.TestProviderRequest{})

	requireStatus(t, err, codes.FailedPrecondition, "AI_PROVIDER_NOT_CONFIGURED")
}

func TestTestProviderWhenTheBudgetOrCapIsSpentSaysWhenItResets(t *testing.T) {
	resets := time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC)
	h := newHarnessWith(t, testerOf(llm.Response{}, &llm.BudgetError{BudgetID: "b-1", ResetsAt: resets}))

	_, err := h.client.TestProvider(h.ctx(t), &fudev1.TestProviderRequest{})

	requireStatus(t, err, codes.ResourceExhausted, "BUDGET_EXHAUSTED")
	if info := errorInfo(err); info == nil || info.GetMetadata()["resets_at"] != "2026-10-31T18:30:00Z" {
		t.Fatalf("info = %v, want resets_at", info)
	}
}

func TestTestProviderWhenMeteringIsDownIsUnavailable(t *testing.T) {
	h := newHarnessWith(t, testerOf(llm.Response{}, fmt.Errorf("%w: %w", llm.ErrMeteringUnavailable, errors.New("down"))))

	_, err := h.client.TestProvider(h.ctx(t), &fudev1.TestProviderRequest{})

	requireStatus(t, err, codes.Unavailable, "METERING_UNAVAILABLE")
}

func TestTestProviderNeedsAnIdentityAndATester(t *testing.T) {
	h := newHarnessWith(t, testerOf(llm.Response{Model: "m"}, nil))
	_, anonymous := h.client.TestProvider(context.Background(), &fudev1.TestProviderRequest{})
	bare := newHarness(t) // no tester wired
	_, unwired := bare.client.TestProvider(bare.ctx(t), &fudev1.TestProviderRequest{})

	if anonymous == nil {
		t.Error("a caller with no identity was answered")
	}
	if unwired == nil {
		t.Error("TestProvider answered with no tester wired")
	}
}

func s(v string) *string { return &v }

func TestTestProviderWithDetailsTestsThemAndReportsLatency(t *testing.T) {
	h := newHarnessWith(t, testerOf(llm.Response{Text: "ok", Model: "my-llama-70b"}, nil))

	res, err := h.client.TestProvider(h.ctx(t), &fudev1.TestProviderRequest{
		BaseUrl: s("https://api.example.com/v1"), Model: s("my-llama-70b"), ApiKey: s("sk-typed-key-9876"),
	})

	if err != nil || !res.GetOk() || res.GetModel() != "my-llama-70b" || res.GetLatencyMs() < 0 {
		t.Fatalf("res %v, err %v; want a working provider", res, err)
	}
}

func TestTestProviderWithDetailsNamesAnAddressThatCannotWork(t *testing.T) {
	h := newHarnessWith(t, testerOf(llm.Response{Model: "m"}, nil))
	tests := map[string]fudev1.ProviderFailure{
		"https://api.example.com/v1/chat/completions": fudev1.ProviderFailure_PROVIDER_FAILURE_ENDPOINT_IN_URL,
		"http://api.example.com/v1":                   fudev1.ProviderFailure_PROVIDER_FAILURE_INVALID_ADDRESS,
	}
	for address, want := range tests {
		t.Run(address, func(t *testing.T) {
			res, err := h.client.TestProvider(h.ctx(t), &fudev1.TestProviderRequest{
				BaseUrl: s(address), Model: s("m"), ApiKey: s("k"),
			})

			if err != nil || res.GetOk() || res.GetFailure() != want {
				t.Fatalf("res %v, err %v; want ok=false with %v", res, err, want)
			}
		})
	}
}

func TestTestProviderWithSomeDetailsOrNoKeyIsRefused(t *testing.T) {
	h := newHarnessWith(t, testerOf(llm.Response{Model: "m"}, nil))
	tests := []struct {
		name   string
		req    *fudev1.TestProviderRequest
		reason string
	}{
		{"only the key", &fudev1.TestProviderRequest{ApiKey: s("k")}, "INVALID_REQUEST"},
		{"address and model", &fudev1.TestProviderRequest{BaseUrl: s("https://a.example.com"), Model: s("m")}, "INVALID_REQUEST"},
		{"blank key", &fudev1.TestProviderRequest{BaseUrl: s("https://a.example.com"), Model: s("m"), ApiKey: s("")}, "API_KEY_REQUIRED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.TestProvider(h.ctx(t), tt.req)

			requireStatus(t, err, codes.InvalidArgument, tt.reason)
		})
	}
}

func TestTestProviderWithDetailsNeverEchoesTheKeyInAnError(t *testing.T) {
	h := newHarnessWith(t, testerOf(llm.Response{}, &llm.BudgetError{BudgetID: "b-1"}))

	_, err := h.client.TestProvider(h.ctx(t), &fudev1.TestProviderRequest{
		BaseUrl: s("https://api.example.com/v1"), Model: s("m"), ApiKey: s("sk-typed-key-9876"),
	})

	if err == nil || strings.Contains(err.Error(), "sk-typed-key-9876") {
		t.Fatalf("err = %v, want a refusal that does not repeat the key", err)
	}
}
