package grpc_test

import (
	"context"
	"errors"
	"fmt"
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
