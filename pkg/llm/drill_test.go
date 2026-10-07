package llm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/llm/llmtest"
)

const drillCallTimeout = 200 * time.Millisecond

// hangingAnthropic counts tokens at once but never answers a message until the
// client gives up, like a model that has stopped responding.
func hangingAnthropic(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var creates atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server only notices a closed connection once the body is read.
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/v1/messages/count_tokens" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"input_tokens": 321}`)
			return
		}
		creates.Add(1)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv, &creates
}

func TestDrillClaudeTimeoutReleasesTheHoldAndSpendsNothing(t *testing.T) {
	// Arrange: the real Anthropic adapter against a model that never answers.
	srv, creates := hangingAnthropic(t)
	api := llm.NewAnthropicAPI("test-key",
		option.WithBaseURL(srv.URL), option.WithRequestTimeout(drillCallTimeout), option.WithMaxRetries(0))
	meter := &llmtest.Meter{ReservationID: "res-timeout"}
	cfg, err := llm.NewConfig("fude", noEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	c, err := llm.New(cfg, api, meter)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	// Act
	start := time.Now()
	_, err = c.Complete(context.Background(), feature, userRequest("write a cover letter"))

	// Assert: it failed promptly, the hold was given back and nothing was spent.
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 10*drillCallTimeout {
		t.Errorf("Complete took %v, want it to give up near %v", elapsed, drillCallTimeout)
	}
	if got := creates.Load(); got != 1 {
		t.Errorf("model calls = %d, want 1", got)
	}
	if len(meter.Reserved) != 1 || len(meter.Committed) != 0 {
		t.Errorf("reserved %d and committed %d, want one hold and no spend", len(meter.Reserved), len(meter.Committed))
	}
	if len(meter.Released) != 1 || meter.Released[0].GetReservationId() != "res-timeout" {
		t.Errorf("released = %v, want res-timeout released once", meter.Released)
	}
}
