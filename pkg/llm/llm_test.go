package llm_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/llm/llmtest"
)

const feature = "fude.cover_letter"

func noEnv(string) (string, bool) { return "", false }

func newClient(t *testing.T, api *llmtest.API, meter *llmtest.Meter, opts ...llm.Option) *llm.Client {
	t.Helper()
	cfg, err := llm.NewConfig("fude", noEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	c, err := llm.New(cfg, api, meter, opts...)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c
}

func userRequest(text string) llm.Request {
	return llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: text}}}
}

func okAPI() *llmtest.API {
	return &llmtest.API{
		Tokens: 1000,
		Result: llm.CallResult{Text: "Dear team", StopReason: "end_turn", Usage: llm.Usage{InputTokens: 900, OutputTokens: 120, CacheReadTokens: 50}},
	}
}

func budgetRefusal() error {
	st, err := status.New(codes.ResourceExhausted, "used up").WithDetails(&errdetails.ErrorInfo{
		Reason:   "BUDGET_EXHAUSTED",
		Metadata: map[string]string{"budget_id": "budget-1", "resets_at": "2026-10-31T18:30:00Z"},
	})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func TestCompleteReservesCallsThenCommitsRealUsage(t *testing.T) {
	// Arrange
	api, meter := okAPI(), &llmtest.Meter{ReservationID: "res-7"}
	c := newClient(t, api, meter)

	// Act
	resp, err := c.Complete(context.Background(), feature, userRequest("write a cover letter"))
	// Assert
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "Dear team" || resp.Model != "claude-opus-5-5" || resp.FromCache {
		t.Errorf("response = %+v", resp)
	}
	if len(meter.Reserved) != 1 || len(api.Created) != 1 || len(meter.Committed) != 1 || len(meter.Released) != 0 {
		t.Fatalf("calls: %d reserves, %d creates, %d commits, %d releases", len(meter.Reserved), len(api.Created), len(meter.Committed), len(meter.Released))
	}
	reserve := meter.Reserved[0]
	if reserve.GetService() != "fude" || reserve.GetFeature() != feature || reserve.GetModel() != "claude-opus-5-5" {
		t.Errorf("reserve = %v", reserve)
	}
	if reserve.GetEstOutputTokens() != 4096 {
		t.Errorf("estimated output = %d, want the feature's 4096 cap", reserve.GetEstOutputTokens())
	}
	commit := meter.Committed[0]
	if commit.GetReservationId() != "res-7" || commit.GetUsage().GetInputTokens() != 900 ||
		commit.GetUsage().GetOutputTokens() != 120 || commit.GetUsage().GetCacheReadTokens() != 50 {
		t.Errorf("commit = %v", commit)
	}
}

func TestCompleteEstimatesInputWithCacheWriteMargin(t *testing.T) {
	tests := []struct {
		name   string
		system string
		want   int32
	}{
		{"without a system prompt the count is exact", "", 1000},
		{"with a system prompt a quarter is added for a cache write", "You write in my voice.", 1250},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, meter := okAPI(), &llmtest.Meter{}
			c := newClient(t, api, meter)
			req := userRequest("hello")
			req.System = tt.system

			if _, err := c.Complete(context.Background(), feature, req); err != nil {
				t.Fatalf("Complete: %v", err)
			}

			if got := meter.Reserved[0].GetEstInputTokens(); got != tt.want {
				t.Errorf("estimated input = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCompleteHonoursARequestMaxTokens(t *testing.T) {
	api, meter := okAPI(), &llmtest.Meter{}
	c := newClient(t, api, meter)
	req := userRequest("short")
	req.MaxTokens = 200

	if _, err := c.Complete(context.Background(), feature, req); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if meter.Reserved[0].GetEstOutputTokens() != 200 || api.Created[0].MaxTokens != 200 {
		t.Errorf("reserved %d and called with %d, want 200 for both", meter.Reserved[0].GetEstOutputTokens(), api.Created[0].MaxTokens)
	}
}

func TestCompleteDeniedByABudgetNeverCallsTheModel(t *testing.T) {
	// Arrange
	api, meter := okAPI(), &llmtest.Meter{ReserveErr: budgetRefusal()}
	c := newClient(t, api, meter)

	// Act
	_, err := c.Complete(context.Background(), feature, userRequest("hi"))

	// Assert
	if !errors.Is(err, llm.ErrBudgetExhausted) {
		t.Fatalf("got %v, want ErrBudgetExhausted", err)
	}
	var refusal *llm.BudgetError
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want a *BudgetError", err)
	}
	if want := time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC); !refusal.ResetsAt.Equal(want) || refusal.BudgetID != "budget-1" {
		t.Errorf("refusal = %+v, want budget-1 resetting at %s", refusal, want)
	}
	if len(api.Created) != 0 || len(meter.Committed) != 0 {
		t.Errorf("a denied call made %d model calls and %d commits", len(api.Created), len(meter.Committed))
	}
}

func TestCompleteFailsClosedWhenSorobanIsDown(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"unavailable", status.Error(codes.Unavailable, "connection refused")},
		{"deadline exceeded", status.Error(codes.DeadlineExceeded, "slow")},
		{"internal error", status.Error(codes.Internal, "boom")},
		{"not a status at all", errors.New("dial tcp: no route")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, meter := okAPI(), &llmtest.Meter{ReserveErr: tt.err}
			c := newClient(t, api, meter)

			_, err := c.Complete(context.Background(), feature, userRequest("hi"))

			if !errors.Is(err, llm.ErrMeteringUnavailable) {
				t.Fatalf("got %v, want ErrMeteringUnavailable", err)
			}
			if errors.Is(err, llm.ErrBudgetExhausted) {
				t.Error("an outage must not look like an exhausted budget")
			}
			if len(api.Created) != 0 {
				t.Errorf("the model was called %d times with no metering", len(api.Created))
			}
		})
	}
}

func TestCompleteFailsWhenTheEstimateCannotBeMade(t *testing.T) {
	api, meter := okAPI(), &llmtest.Meter{}
	api.CountErr = errors.New("count_tokens down")
	c := newClient(t, api, meter)

	_, err := c.Complete(context.Background(), feature, userRequest("hi"))

	if err == nil || len(meter.Reserved) != 0 || len(api.Created) != 0 {
		t.Fatalf("err = %v, %d reserves, %d creates; want an error and neither", err, len(meter.Reserved), len(api.Created))
	}
}

func TestCompleteReleasesTheReservationWhenTheCallFails(t *testing.T) {
	// Arrange
	api, meter := okAPI(), &llmtest.Meter{ReservationID: "res-9"}
	api.CreateErr = errors.New("overloaded")
	c := newClient(t, api, meter)

	// Act
	_, err := c.Complete(context.Background(), feature, userRequest("hi"))

	// Assert
	if err == nil {
		t.Fatal("want the API error")
	}
	if len(meter.Released) != 1 || meter.Released[0].GetReservationId() != "res-9" || len(meter.Committed) != 0 {
		t.Errorf("released %v and committed %d, want res-9 released and nothing committed", meter.Released, len(meter.Committed))
	}
}

func TestCompleteSettlesEvenWhenTheCallerGivesUp(t *testing.T) {
	// Arrange: the caller's context is cancelled while the model is answering.
	ctx, cancel := context.WithCancel(context.Background())
	api, meter := okAPI(), &llmtest.Meter{}
	api.OnCreate = func(context.Context) { cancel() }
	c := newClient(t, api, meter)

	// Act
	_, err := c.Complete(ctx, feature, userRequest("hi"))
	// Assert
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(meter.Committed) != 1 || meter.CommitCtxErrs[0] != nil {
		t.Errorf("commit ran %d times with context error %v, want once with a live context", len(meter.Committed), meter.CommitCtxErrs)
	}
}

func TestCompleteRetriesACommitAndKeepsTheAnswer(t *testing.T) {
	tests := []struct {
		name        string
		failCommits int
		wantCommits int
	}{
		{"one failure is retried", 1, 2},
		{"persistent failure stops after three tries", 10, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, meter := okAPI(), &llmtest.Meter{FailCommits: tt.failCommits}
			c := newClient(t, api, meter, llm.WithLogger(slog.New(slog.DiscardHandler)))

			resp, err := c.Complete(context.Background(), feature, userRequest("hi"))

			if err != nil || resp.Text != "Dear team" {
				t.Fatalf("got %+v, %v; the paid-for answer must be kept", resp, err)
			}
			if len(meter.Committed) != tt.wantCommits {
				t.Errorf("commit tried %d times, want %d", len(meter.Committed), tt.wantCommits)
			}
		})
	}
}

func TestCompleteMetersARefusalAndReturnsAnError(t *testing.T) {
	api, meter := okAPI(), &llmtest.Meter{}
	api.Result = llm.CallResult{Refused: true, StopReason: "refusal", Usage: llm.Usage{InputTokens: 800, OutputTokens: 3}}
	c := newClient(t, api, meter)

	_, err := c.Complete(context.Background(), feature, userRequest("hi"))

	if !errors.Is(err, llm.ErrRefused) {
		t.Fatalf("got %v, want ErrRefused", err)
	}
	if len(meter.Committed) != 1 || meter.Committed[0].GetUsage().GetInputTokens() != 800 {
		t.Errorf("committed %v, want the refusal's usage metered", meter.Committed)
	}
}

func TestCompleteRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name    string
		feature string
		req     llm.Request
		want    error
	}{
		{"unknown feature", "fude.unknown", userRequest("hi"), llm.ErrUnknownFeature},
		{"another service's feature", "katana.suggest", userRequest("hi"), llm.ErrUnknownFeature},
		{"no messages", feature, llm.Request{}, llm.ErrInvalidRequest},
		{"bad role", feature, llm.Request{Messages: []llm.Message{{Role: "system", Content: "x"}}}, llm.ErrInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, meter := okAPI(), &llmtest.Meter{}
			c := newClient(t, api, meter)

			_, err := c.Complete(context.Background(), tt.feature, tt.req)

			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			if len(meter.Reserved) != 0 {
				t.Error("a bad request reserved budget")
			}
		})
	}
}

func TestCompleteServesAnIdenticalRequestFromCache(t *testing.T) {
	// Arrange
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	cache := llm.NewMemoryCache(func() time.Time { return now })
	api, meter := okAPI(), &llmtest.Meter{}
	c := newClient(t, api, meter, llm.WithCache(cache))
	req := userRequest("classify this")
	req.Cache = true

	// Act
	first, err := c.Complete(context.Background(), feature, req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := c.Complete(context.Background(), feature, req)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	now = now.Add(llm.DefaultCacheTTL + time.Second)
	third, err := c.Complete(context.Background(), feature, req)
	if err != nil {
		t.Fatalf("third: %v", err)
	}

	// Assert
	if first.FromCache || !second.FromCache || third.FromCache {
		t.Errorf("FromCache = %v, %v, %v; want false, true, false (expired)", first.FromCache, second.FromCache, third.FromCache)
	}
	if len(meter.Reserved) != 2 || len(api.Created) != 2 {
		t.Errorf("%d reserves and %d model calls, want 2 and 2: the hit costs nothing", len(meter.Reserved), len(api.Created))
	}
}

func TestCompleteDoesNotCacheUnlessAskedTo(t *testing.T) {
	cache := llm.NewMemoryCache(nil)
	api, meter := okAPI(), &llmtest.Meter{}
	c := newClient(t, api, meter, llm.WithCache(cache))

	for range 2 {
		if _, err := c.Complete(context.Background(), feature, userRequest("same")); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}

	if len(api.Created) != 2 {
		t.Errorf("%d model calls, want 2: Cache was not set", len(api.Created))
	}
}

func TestDifferentRequestsDoNotShareACacheEntry(t *testing.T) {
	cache := llm.NewMemoryCache(nil)
	api, meter := okAPI(), &llmtest.Meter{}
	c := newClient(t, api, meter, llm.WithCache(cache))
	a, b := userRequest("one"), userRequest("two")
	a.Cache, b.Cache = true, true

	for _, req := range []llm.Request{a, b} {
		if _, err := c.Complete(context.Background(), feature, req); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}

	if len(api.Created) != 2 {
		t.Errorf("%d model calls, want 2", len(api.Created))
	}
}
