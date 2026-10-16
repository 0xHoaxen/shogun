package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
)

func TestTestProviderMakesOneSmallMeteredCallAndReportsTheModel(t *testing.T) {
	model := &fakeLLM{text: "ok"}

	res, err := app.NewProviderTester(model).Test(context.Background())

	if err != nil || !res.OK || res.Model != "claude-test" || res.Failure != "" {
		t.Fatalf("res %+v, err %v; want a working provider", res, err)
	}
	if model.calls != 1 || model.feature != "fude.test_connection" {
		t.Fatalf("%d calls on feature %q, want one call on fude.test_connection", model.calls, model.feature)
	}
	if len(model.req.Messages) != 1 || len(model.req.Messages[0].Content) > 100 {
		t.Fatalf("request = %+v, want one short message", model.req)
	}
	if model.req.Cache {
		t.Fatal("a connection test must not be served from the cache")
	}
}

func TestTestProviderReportsAProviderThatFailsAsAnAnswerNotAnError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want llm.FailureKind
	}{
		{"not public", fmt.Errorf("claude call: %w", llmNotPublic()), llm.FailureNotPublic},
		{"anything else", fmt.Errorf("claude call: %w", errors.New("boom")), llm.FailureOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := app.NewProviderTester(&fakeLLM{err: tt.err}).Test(context.Background())

			if err != nil || res.OK || res.Failure != tt.want || res.Model != "" {
				t.Fatalf("res %+v, err %v; want failure %q reported as a result", res, err, tt.want)
			}
		})
	}
}

func TestTestProviderPassesOnWhatTheUserMustFixOrWaitOutAsErrors(t *testing.T) {
	// These are not a provider's fault: no provider set, a spent budget or cap, metering down.
	for name, cause := range map[string]error{
		"no provider":         llm.ErrProviderNotConfigured,
		"budget or cap":       &llm.BudgetError{},
		"metering down":       llm.ErrMeteringUnavailable,
		"wrapped no provider": fmt.Errorf("claude call: %w", llm.ErrProviderNotConfigured),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := app.NewProviderTester(&fakeLLM{err: cause}).Test(context.Background())

			if err == nil || !errors.Is(err, cause) {
				t.Fatalf("err = %v, want %v passed on", err, cause)
			}
		})
	}
}
