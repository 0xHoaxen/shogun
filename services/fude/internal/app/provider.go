package app

import (
	"context"
	"errors"

	"github.com/0xHoaxen/shogun/pkg/llm"
)

// featureTestConnection is the metered feature behind "test connection".
const featureTestConnection = "fude.test_connection"

// testPrompt is as short as a prompt gets; any answer proves the connection.
const testPrompt = "Reply with the single word: ok"

// ProviderTestResult is what a connection test found out.
type ProviderTestResult struct {
	OK bool
	// Failure says why a provider that was reached did not work; empty when OK.
	Failure llm.FailureKind
	// Model is the model that answered, when OK.
	Model string
}

// ProviderTester checks the caller's own AI provider.
type ProviderTester struct {
	llm Completer
}

// NewProviderTester returns a ProviderTester that calls through c.
func NewProviderTester(c Completer) *ProviderTester { return &ProviderTester{llm: c} }

// Test makes one small call with the caller's own provider, metered like any
// other. A provider that does not work is a result, not an error. What the user
// must fix or wait out is returned as an error: no provider set, a spent budget
// or token cap, or metering down. The provider's own words are never returned.
func (p *ProviderTester) Test(ctx context.Context) (ProviderTestResult, error) {
	resp, err := p.llm.Complete(ctx, featureTestConnection, llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: testPrompt}},
	})
	switch {
	case err == nil:
		return ProviderTestResult{OK: true, Model: resp.Model}, nil
	case errors.Is(err, llm.ErrRefused):
		// The provider answered, so the connection works.
		return ProviderTestResult{OK: true, Model: resp.Model}, nil
	case errors.Is(err, llm.ErrProviderNotConfigured), errors.Is(err, llm.ErrBudgetExhausted), errors.Is(err, llm.ErrMeteringUnavailable):
		return ProviderTestResult{}, err
	default:
		return ProviderTestResult{Failure: llm.ClassifyFailure(err)}, nil
	}
}
