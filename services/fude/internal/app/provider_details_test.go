package app_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
)

const typedKey = "sk-typed-key-9876"

func details(baseURL string) app.ProviderDetails {
	return app.ProviderDetails{BaseURL: baseURL, Model: "my-llama-70b", APIKey: typedKey}
}

// steppingClock moves 250ms on every reading, so a call measures as 250ms.
func steppingClock() func() time.Time {
	t := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	return func() time.Time {
		t = t.Add(250 * time.Millisecond)
		return t
	}
}

func TestTestDetailsCallsTheTypedProviderOnceAndReportsModelAndLatency(t *testing.T) {
	// Arrange
	model := &fakeLLM{text: "ok"}
	tester := app.NewProviderTester(model, app.WithTesterClock(steppingClock()))

	// Act
	res, err := tester.TestDetails(context.Background(), details("https://API.Example.com/v1/"))

	// Assert
	if err != nil || !res.OK || res.Model != "my-llama-70b" || res.Failure != "" || res.Latency != 250*time.Millisecond {
		t.Fatalf("res %+v, err %v; want ok, the model that answered and 250ms", res, err)
	}
	want := llm.Access{BaseURL: "https://api.example.com/v1", Model: "my-llama-70b", APIKey: typedKey}
	if model.calls != 1 || model.feature != "fude.test_connection" || model.access != want {
		t.Fatalf("%d calls on %q with %+v, want one call with the tidied typed details", model.calls, model.feature, model.access)
	}
	if model.req.Cache {
		t.Fatal("a connection test must not be served from the cache")
	}
}

func TestTestDetailsNamesEachWayAProviderCanFailWithoutItsOwnWords(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want llm.FailureKind
	}{
		{"key refused", &llm.StatusError{StatusCode: 401}, llm.FailureRejected},
		{"rate limited", &llm.StatusError{StatusCode: 429}, llm.FailureRateLimited},
		{"unknown model or path", &llm.StatusError{StatusCode: 404}, llm.FailureNotFound},
		{"provider down", &llm.StatusError{StatusCode: 503}, llm.FailureProviderError},
		{"too slow", context.DeadlineExceeded, llm.FailureUnreachable},
		{"not public", llmNotPublic(), llm.FailureNotPublic},
		{"anything else", errors.New("boom " + typedKey), llm.FailureOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := app.NewProviderTester(&fakeLLM{err: fmt.Errorf("claude call: %w", tt.err)}).
				TestDetails(context.Background(), details("https://api.example.com/v1"))

			if err != nil || res.OK || res.Failure != tt.want || res.Model != "" {
				t.Fatalf("res %+v, err %v; want failure %q as a result", res, err, tt.want)
			}
			if strings.Contains(fmt.Sprintf("%+v", res), typedKey) {
				t.Fatal("the result repeats the key")
			}
		})
	}
}

func TestTestDetailsMakesNoCallForAnAddressThatCannotWork(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    llm.FailureKind
	}{
		{"endpoint already in it", "https://api.example.com/v1/chat/completions", app.FailureEndpointInURL},
		{"endpoint with a slash", "https://api.example.com/v1/chat/completions/", app.FailureEndpointInURL},
		{"endpoint in any case", "https://api.example.com/V1/Chat/Completions", app.FailureEndpointInURL},
		{"plain http", "http://api.example.com/v1", app.FailureInvalidAddress},
		{"no scheme", "api.example.com/v1", app.FailureInvalidAddress},
		{"other scheme", "ftp://api.example.com", app.FailureInvalidAddress},
		{"credentials in it", "https://user:pass@api.example.com/v1", app.FailureInvalidAddress},
		{"query in it", "https://api.example.com/v1?key=abc", app.FailureInvalidAddress},
		{"no host", "https:///v1", app.FailureInvalidAddress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &fakeLLM{text: "ok"}

			res, err := app.NewProviderTester(model).TestDetails(context.Background(), details(tt.address))

			if err != nil || res.OK || res.Failure != tt.want {
				t.Fatalf("res %+v, err %v; want failure %q", res, err, tt.want)
			}
			if model.calls != 0 {
				t.Fatalf("%d calls, want none for an address that cannot work", model.calls)
			}
		})
	}
}

func TestTestDetailsAllowsPlainHttpOnlyWhenToldTo(t *testing.T) {
	model := &fakeLLM{text: "ok"}

	res, err := app.NewProviderTester(model, app.AllowInsecureAddresses()).
		TestDetails(context.Background(), details("http://localhost:9999/v1"))

	if err != nil || !res.OK || model.access.BaseURL != "http://localhost:9999/v1" {
		t.Fatalf("res %+v, err %v, access %+v; want the local address tested", res, err, model.access)
	}
}

func TestTestDetailsRequiresKeyModelAndAddressWithoutCalling(t *testing.T) {
	tests := []struct {
		name   string
		in     app.ProviderDetails
		reason string
	}{
		{"no key", app.ProviderDetails{BaseURL: "https://a.example.com", Model: "m"}, "API_KEY_REQUIRED"},
		{"blank key", app.ProviderDetails{BaseURL: "https://a.example.com", Model: "m", APIKey: "   "}, "API_KEY_REQUIRED"},
		{"no model", app.ProviderDetails{BaseURL: "https://a.example.com", APIKey: "k"}, "INVALID_REQUEST"},
		{"no address", app.ProviderDetails{Model: "m", APIKey: "k"}, "INVALID_REQUEST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &fakeLLM{text: "ok"}

			_, err := app.NewProviderTester(model).TestDetails(context.Background(), tt.in)

			var bad *app.InvalidArgumentError
			if !errors.As(err, &bad) || bad.Reason != tt.reason || model.calls != 0 {
				t.Fatalf("err %v, %d calls; want reason %q and no call", err, model.calls, tt.reason)
			}
		})
	}
}

func TestTestDetailsPassesOnWhatTheUserMustFixOrWaitOutAsErrors(t *testing.T) {
	for name, cause := range map[string]error{
		"budget or cap": &llm.BudgetError{},
		"metering down": llm.ErrMeteringUnavailable,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := app.NewProviderTester(&fakeLLM{err: cause}).TestDetails(context.Background(), details("https://api.example.com/v1"))

			if !errors.Is(err, cause) {
				t.Fatalf("err = %v, want %v passed on", err, cause)
			}
		})
	}
}

func TestAProviderThatRefusesThePromptStillProvedTheConnection(t *testing.T) {
	res, err := app.NewProviderTester(&fakeLLM{err: llm.ErrRefused}).TestDetails(context.Background(), details("https://api.example.com/v1"))

	if err != nil || !res.OK {
		t.Fatalf("res %+v, err %v; want ok: the provider answered", res, err)
	}
}

func TestDetailsOrNilNeedsAllThreeOrNone(t *testing.T) {
	s := func(v string) *string { return &v }
	tests := []struct {
		name    string
		in      [3]*string
		wantNil bool
		wantErr bool
	}{
		{"none means the saved provider", [3]*string{}, true, false},
		{"all three", [3]*string{s("u"), s("m"), s("k")}, false, false},
		{"empty strings still count as sent", [3]*string{s(""), s(""), s("")}, false, false},
		{"only the key", [3]*string{nil, nil, s("k")}, true, true},
		{"address and model", [3]*string{s("u"), s("m"), nil}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := app.DetailsOrNil(tt.in[0], tt.in[1], tt.in[2])

			if (got == nil) != tt.wantNil || (err != nil) != tt.wantErr {
				t.Fatalf("got %+v, err %v", got, err)
			}
		})
	}
}
