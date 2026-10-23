package llm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/llm/llmtest"
)

const drillCallTimeout = 200 * time.Millisecond

// hangingProvider never answers a message until the client gives up, like a
// model that has stopped responding.
func hangingProvider(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var creates atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// The server only notices a closed connection once the body is read.
		_, _ = io.Copy(io.Discard, r.Body)
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
	// Arrange: the real provider adapter against a model that never answers.
	srv, creates := hangingProvider(t)
	api := llm.NewOpenAIAPI(llm.AllowPrivateAddresses(), llm.CallTimeout(drillCallTimeout), llm.MaxRetries(0))
	meter := &llmtest.Meter{
		ReservationID: "res-timeout",
		Provider:      &sorobanv1.ProviderAccess{BaseUrl: srv.URL, Model: "claude-opus-5-5", ApiKey: "test-key-12345"},
	}
	cfg, err := llm.NewConfig("fude")
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
