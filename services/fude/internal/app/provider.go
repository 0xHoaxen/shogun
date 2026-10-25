package app

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/0xHoaxen/shogun/pkg/llm"
)

// featureTestConnection is the metered feature behind "test connection".
const featureTestConnection = "fude.test_connection"

// testPrompt is as short as a prompt gets; any answer proves the connection.
const testPrompt = "Reply with the single word: ok"

// Failures a test can find without making a call. They sit beside llm's kinds.
const (
	// FailureEndpointInURL means the address already ends in /chat/completions,
	// which is added to it.
	FailureEndpointInURL llm.FailureKind = "endpoint_in_url"
	// FailureInvalidAddress means the address is not one that can be called.
	FailureInvalidAddress llm.FailureKind = "invalid_address"
)

// Reasons of the InvalidArgumentError a provider test returns.
const (
	ReasonAPIKeyRequired  = "API_KEY_REQUIRED"
	ReasonInvalidRequest  = "INVALID_REQUEST"
	reasonDetailsTogether = "send the address, model and key together"
)

// chatEndpoint is the path Shogun adds to a provider's address.
const chatEndpoint = "/chat/completions"

// ProviderTestResult is what a connection test found out.
type ProviderTestResult struct {
	OK bool
	// Failure says why a provider that was reached did not work; empty when OK.
	Failure llm.FailureKind
	// Model is the model that answered, when OK.
	Model string
	// Latency is how long the provider took, when a call was made.
	Latency time.Duration
}

// ProviderDetails are the address, model and key a user typed and has not saved.
type ProviderDetails struct {
	BaseURL string
	Model   string
	APIKey  string
}

// TestCompleter is what a ProviderTester calls through: the metered client, with
// the saved provider or with details it is given.
type TestCompleter interface {
	Completer
	CompleteWith(ctx context.Context, feature string, access llm.Access, req llm.Request) (llm.Response, error)
}

// ProviderTester checks an AI provider: the caller's saved one, or details the
// caller has typed and not saved.
type ProviderTester struct {
	llm      TestCompleter
	insecure bool
	now      func() time.Time
}

// TesterOption changes a ProviderTester.
type TesterOption func(*ProviderTester)

// AllowInsecureAddresses lets a tested address be plain http, so a fake provider
// on localhost works in local development and tests. Never use it in production.
func AllowInsecureAddresses() TesterOption { return func(p *ProviderTester) { p.insecure = true } }

// WithTesterClock sets the clock latency is measured with.
func WithTesterClock(now func() time.Time) TesterOption {
	return func(p *ProviderTester) { p.now = now }
}

// NewProviderTester returns a ProviderTester that calls through c.
func NewProviderTester(c TestCompleter, opts ...TesterOption) *ProviderTester {
	p := &ProviderTester{llm: c, now: time.Now}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

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

// DetailsOrNil returns the details to test, or nil when none of the three was
// sent (test the saved provider). Some but not all is an error.
func DetailsOrNil(baseURL, model, apiKey *string) (*ProviderDetails, error) {
	set := 0
	for _, v := range []*string{baseURL, model, apiKey} {
		if v != nil {
			set++
		}
	}
	switch set {
	case 0:
		return nil, nil
	case 3:
		return &ProviderDetails{BaseURL: *baseURL, Model: *model, APIKey: *apiKey}, nil
	default:
		return nil, invalidField("", ReasonInvalidRequest, "%s", reasonDetailsTogether)
	}
}

// TestDetails makes one small call with details the user typed and has not
// saved, metered like any other. The key is used for this call only: it is not
// stored, logged or returned, and soroban is told the model name alone. An
// address that cannot work is a result and no call is made. A provider that
// does not work is a result too; what the user must fix or wait out (a missing
// key, a spent budget or cap, metering down) is returned as an error.
func (p *ProviderTester) TestDetails(ctx context.Context, in ProviderDetails) (ProviderTestResult, error) {
	in = ProviderDetails{BaseURL: strings.TrimSpace(in.BaseURL), Model: strings.TrimSpace(in.Model), APIKey: strings.TrimSpace(in.APIKey)}
	if in.APIKey == "" {
		return ProviderTestResult{}, invalidField("api_key", ReasonAPIKeyRequired, "an API key is required to test")
	}
	if in.BaseURL == "" || in.Model == "" {
		return ProviderTestResult{}, invalidField("", ReasonInvalidRequest, "an address and a model are required to test")
	}
	address, failure := p.checkAddress(in.BaseURL)
	if failure != "" {
		return ProviderTestResult{Failure: failure}, nil
	}

	started := p.now()
	resp, err := p.llm.CompleteWith(ctx, featureTestConnection,
		llm.Access{BaseURL: address, Model: in.Model, APIKey: in.APIKey},
		llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: testPrompt}}})
	return p.result(resp, err, p.now().Sub(started))
}

// result turns the outcome of a call into a ProviderTestResult.
func (p *ProviderTester) result(resp llm.Response, err error, latency time.Duration) (ProviderTestResult, error) {
	switch {
	case err == nil, errors.Is(err, llm.ErrRefused):
		// A refusal means the provider answered, so the connection works.
		return ProviderTestResult{OK: true, Model: resp.Model, Latency: latency}, nil
	case errors.Is(err, llm.ErrProviderNotConfigured), errors.Is(err, llm.ErrBudgetExhausted), errors.Is(err, llm.ErrMeteringUnavailable):
		return ProviderTestResult{}, err
	default:
		return ProviderTestResult{Failure: llm.ClassifyFailure(err), Latency: latency}, nil
	}
}

// checkAddress returns the address in its normal form, or the failure that
// stops a call. Whether the host is public is left to the HTTP client, which
// refuses private and local addresses when it connects (not_public).
func (p *ProviderTester) checkAddress(raw string) (string, llm.FailureKind) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", FailureInvalidAddress
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !p.insecure) {
		return "", FailureInvalidAddress
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	if strings.HasSuffix(strings.ToLower(u.Path), chatEndpoint) {
		return "", FailureEndpointInURL
	}
	return u.String(), ""
}
